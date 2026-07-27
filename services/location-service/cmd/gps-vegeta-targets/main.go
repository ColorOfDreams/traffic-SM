package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const csvTimeLayout = "1/2/2006 15:04"

type config struct {
	inputPath        string
	targetURL        string
	runID            string
	cycles           int
	maxDrivers       int
	eventsPerDriver  int
	initialTimeShift time.Duration
	shardIndex       int
	shardCount       int
}

type point struct {
	recordedAtMS int64
	longitude    float64
	latitude     float64
	speedMPS     float64
	headingDeg   float64
	accuracyM    float64
}

type driverTrace struct {
	id     string
	points []point
}

type canonicalEvent struct {
	EventID      string  `json:"event_id"`
	DriverID     string  `json:"driver_id"`
	RecordedAtMS int64   `json:"recorded_at_ms"`
	Longitude    float64 `json:"longitude"`
	Latitude     float64 `json:"latitude"`
	SpeedMPS     float64 `json:"speed_mps"`
	HeadingDeg   float64 `json:"heading_deg"`
	AccuracyM    float64 `json:"accuracy_m"`
}

// vegetaTarget là định dạng JSON target mà `vegeta attack -format=json -lazy`
// nhận từ stdin. []byte được encoding/json chuyển thành base64 theo yêu cầu.
type vegetaTarget struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Header map[string][]string `json:"header"`
	Body   []byte              `json:"body"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseFlags()
	if err != nil {
		return err
	}

	drivers, minimumTime, maximumTime, err := readCSV(cfg)
	if err != nil {
		return err
	}
	if len(drivers) == 0 {
		return errors.New("CSV does not contain any usable GPS events")
	}
	drivers = selectDriverShard(drivers, cfg.shardIndex, cfg.shardCount)
	if len(drivers) == 0 {
		return fmt.Errorf(
			"driver shard %d/%d does not contain any drivers",
			cfg.shardIndex,
			cfg.shardCount,
		)
	}

	cycleSpan := maximumTime - minimumTime + int64(time.Minute/time.Millisecond)
	if cycleSpan <= 0 {
		return errors.New("could not calculate a positive CSV time span")
	}

	output := bufio.NewWriterSize(os.Stdout, 1024*1024)
	defer output.Flush()
	encoder := json.NewEncoder(output)

	headers := map[string][]string{
		"Content-Type": {"application/json"},
	}

	for cycle := 0; cfg.cycles == 0 || cycle < cfg.cycles; cycle++ {
		timeShift := cfg.initialTimeShift.Milliseconds() + int64(cycle)*cycleSpan
		if err := writeCycle(encoder, cfg, drivers, headers, cycle, timeShift); err != nil {
			// Vegeta đóng stdin khi hết duration. Đây là kết thúc bình thường
			// đối với generator chạy --cycles=0 trong pipeline.
			if errors.Is(err, syscall.EPIPE) {
				return nil
			}
			return err
		}
	}
	return nil
}

func parseFlags() (config, error) {
	var cfg config
	flag.StringVar(&cfg.inputPath, "input", "", "path to fake_gps.csv")
	flag.StringVar(&cfg.targetURL, "url", "http://host.docker.internal:8080/v1/gps-events", "location-service GPS endpoint")
	flag.StringVar(&cfg.runID, "run-id", strconv.FormatInt(time.Now().UnixNano(), 36), "unique benchmark run identifier")
	flag.IntVar(&cfg.cycles, "cycles", 1, "number of complete CSV cycles; 0 streams forever")
	flag.IntVar(&cfg.maxDrivers, "max-drivers", 0, "maximum drivers to include; 0 includes all")
	flag.IntVar(&cfg.eventsPerDriver, "events-per-driver", 0, "maximum events per driver; 0 includes all")
	flag.DurationVar(
		&cfg.initialTimeShift,
		"time-shift",
		0,
		"duration added to every CSV timestamp, for example 24h",
	)
	flag.IntVar(&cfg.shardIndex, "shard-index", 0, "zero-based driver shard index")
	flag.IntVar(&cfg.shardCount, "shard-count", 1, "total number of driver shards")
	flag.Parse()

	cfg.inputPath = strings.TrimSpace(cfg.inputPath)
	cfg.targetURL = strings.TrimSpace(cfg.targetURL)
	cfg.runID = strings.TrimSpace(cfg.runID)
	if cfg.inputPath == "" {
		return config{}, errors.New("--input is required")
	}
	if cfg.targetURL == "" {
		return config{}, errors.New("--url is required")
	}
	if cfg.runID == "" {
		return config{}, errors.New("--run-id is required")
	}
	if cfg.cycles < 0 {
		return config{}, errors.New("--cycles must not be negative")
	}
	if cfg.maxDrivers < 0 {
		return config{}, errors.New("--max-drivers must not be negative")
	}
	if cfg.eventsPerDriver < 0 {
		return config{}, errors.New("--events-per-driver must not be negative")
	}
	if cfg.shardCount < 1 {
		return config{}, errors.New("--shard-count must be positive")
	}
	if cfg.shardIndex < 0 || cfg.shardIndex >= cfg.shardCount {
		return config{}, errors.New("--shard-index must be from zero up to shard-count minus one")
	}
	return cfg, nil
}

func selectDriverShard(drivers []driverTrace, shardIndex int, shardCount int) []driverTrace {
	if shardCount == 1 {
		return drivers
	}
	shard := make([]driverTrace, 0, (len(drivers)+shardCount-1)/shardCount)
	for index, driver := range drivers {
		if index%shardCount == shardIndex {
			shard = append(shard, driver)
		}
	}
	return shard
}

func readCSV(cfg config) ([]driverTrace, int64, int64, error) {
	file, err := os.Open(cfg.inputPath)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("open CSV: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReaderSize(file, 1024*1024))
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read CSV header: %w", err)
	}

	columns, err := requiredColumns(header)
	if err != nil {
		return nil, 0, 0, err
	}

	driverIndexes := make(map[string]int)
	var drivers []driverTrace
	var minimumTime int64
	var maximumTime int64

	for line := 2; ; line++ {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, 0, 0, fmt.Errorf("read CSV line %d: %w", line, readErr)
		}

		driverID := strings.TrimSpace(record[columns["driver_id"]])
		if driverID == "" {
			return nil, 0, 0, fmt.Errorf("CSV line %d: driver_id is empty", line)
		}

		driverIndex, exists := driverIndexes[driverID]
		if !exists {
			if cfg.maxDrivers > 0 && len(drivers) >= cfg.maxDrivers {
				continue
			}
			driverIndex = len(drivers)
			driverIndexes[driverID] = driverIndex
			drivers = append(drivers, driverTrace{id: driverID})
		}
		driver := &drivers[driverIndex]
		if cfg.eventsPerDriver > 0 && len(driver.points) >= cfg.eventsPerDriver {
			continue
		}

		gpsPoint, err := parsePoint(record, columns)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("CSV line %d: %w", line, err)
		}
		driver.points = append(driver.points, gpsPoint)

		if minimumTime == 0 || gpsPoint.recordedAtMS < minimumTime {
			minimumTime = gpsPoint.recordedAtMS
		}
		if gpsPoint.recordedAtMS > maximumTime {
			maximumTime = gpsPoint.recordedAtMS
		}
	}

	nonEmpty := drivers[:0]
	for _, driver := range drivers {
		if len(driver.points) > 0 {
			nonEmpty = append(nonEmpty, driver)
		}
	}
	return nonEmpty, minimumTime, maximumTime, nil
}

func requiredColumns(header []string) (map[string]int, error) {
	indexes := make(map[string]int, len(header))
	for index, name := range header {
		indexes[strings.TrimSpace(name)] = index
	}

	required := []string{
		"driver_id",
		"t_timestamp",
		"lat",
		"lng",
		"bearing",
		"horizontal_acc",
		"speed",
		"time",
	}
	for _, name := range required {
		if _, exists := indexes[name]; !exists {
			return nil, fmt.Errorf("CSV is missing required column %q", name)
		}
	}
	return indexes, nil
}

func parsePoint(record []string, columns map[string]int) (point, error) {
	recordedAtMS, err := parseRecordedAtMS(
		record[columns["time"]],
		record[columns["t_timestamp"]],
	)
	if err != nil {
		return point{}, err
	}

	longitude, err := requiredFloat(record[columns["lng"]], "lng")
	if err != nil {
		return point{}, err
	}
	latitude, err := requiredFloat(record[columns["lat"]], "lat")
	if err != nil {
		return point{}, err
	}
	speed, err := optionalFloat(record[columns["speed"]])
	if err != nil {
		return point{}, fmt.Errorf("speed: %w", err)
	}
	heading, err := optionalFloat(record[columns["bearing"]])
	if err != nil {
		return point{}, fmt.Errorf("bearing: %w", err)
	}
	accuracy, err := optionalFloat(record[columns["horizontal_acc"]])
	if err != nil {
		return point{}, fmt.Errorf("horizontal_acc: %w", err)
	}
	if longitude < -180 || longitude > 180 {
		return point{}, errors.New("lng must be from -180 to 180")
	}
	if latitude < -90 || latitude > 90 {
		return point{}, errors.New("lat must be from -90 to 90")
	}
	if speed != -1 && speed < 0 {
		return point{}, errors.New("speed must be -1 or non-negative")
	}
	if heading != -1 && (heading < 0 || heading >= 360) {
		return point{}, errors.New("bearing must be -1 or from 0 up to 360")
	}
	if accuracy != -1 && accuracy < 0 {
		return point{}, errors.New("horizontal_acc must be -1 or non-negative")
	}

	return point{
		recordedAtMS: recordedAtMS,
		longitude:    longitude,
		latitude:     latitude,
		speedMPS:     speed,
		headingDeg:   heading,
		accuracyM:    accuracy,
	}, nil
}

func parseRecordedAtMS(dateAndMinute string, traceTimestamp string) (int64, error) {
	vietnamTime := time.FixedZone("Asia/Ho_Chi_Minh", 7*60*60)
	base, err := time.ParseInLocation(csvTimeLayout, strings.TrimSpace(dateAndMinute), vietnamTime)
	if err != nil {
		return 0, fmt.Errorf("time %q is invalid: %w", dateAndMinute, err)
	}

	parts := strings.Split(strings.TrimSpace(traceTimestamp), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("t_timestamp %q must use MM:SS.s format", traceTimestamp)
	}
	seconds, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || seconds < 0 || seconds >= 60 {
		return 0, fmt.Errorf("t_timestamp %q contains invalid seconds", traceTimestamp)
	}

	milliseconds := int64(math.Round(seconds * 1000))
	return base.UnixMilli() + milliseconds, nil
}

func requiredFloat(value string, field string) (float64, error) {
	number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("%s %q is invalid: %w", field, value, err)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%s %q must be finite", field, value)
	}
	return number, nil
}

func optionalFloat(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return -1, nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is invalid: %w", value, err)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, fmt.Errorf("%q must be finite", value)
	}
	return number, nil
}

func writeCycle(
	encoder *json.Encoder,
	cfg config,
	drivers []driverTrace,
	headers map[string][]string,
	cycle int,
	timeShift int64,
) error {
	maximumEvents := 0
	for _, driver := range drivers {
		if len(driver.points) > maximumEvents {
			maximumEvents = len(driver.points)
		}
	}

	for sequence := 0; sequence < maximumEvents; sequence++ {
		for _, driver := range drivers {
			if sequence >= len(driver.points) {
				continue
			}
			gpsPoint := driver.points[sequence]
			event := canonicalEvent{
				EventID: fmt.Sprintf(
					"vegeta-%s-%d-%s-%d",
					cfg.runID,
					cycle,
					driver.id,
					sequence,
				),
				DriverID:     driver.id,
				RecordedAtMS: gpsPoint.recordedAtMS + timeShift,
				Longitude:    gpsPoint.longitude,
				Latitude:     gpsPoint.latitude,
				SpeedMPS:     gpsPoint.speedMPS,
				HeadingDeg:   gpsPoint.headingDeg,
				AccuracyM:    gpsPoint.accuracyM,
			}
			body, err := json.Marshal(event)
			if err != nil {
				return fmt.Errorf("encode event %s: %w", event.EventID, err)
			}

			target := vegetaTarget{
				Method: "POST",
				URL:    cfg.targetURL,
				Header: headers,
				Body:   body,
			}
			if err := encoder.Encode(target); err != nil {
				return fmt.Errorf("write Vegeta target: %w", err)
			}
		}
	}
	return nil
}
