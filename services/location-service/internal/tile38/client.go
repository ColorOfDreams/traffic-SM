// Package tile38 cung cấp TCP client tối thiểu để gửi lệnh RESP tới Tile38.
package tile38

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// commandTimeout ngăn importer treo vô hạn khi Tile38 hoặc Docker network
// không phản hồi.
const commandTimeout = 5 * time.Second

// Client giữ một kết nối TCP và buffer đọc/ghi. Importer chạy tuần tự nên một
// kết nối là đủ; Client hiện chưa được thiết kế để nhiều goroutine dùng chung.
type Client struct {
	connection net.Conn
	reader     *bufio.Reader
	writer     *bufio.Writer
}

// Dial mở một kết nối TCP tới Tile38.
func Dial(address string) (*Client, error) {
	connection, err := net.DialTimeout("tcp", address, commandTimeout)
	if err != nil {
		return nil, fmt.Errorf("connect to Tile38: %w", err)
	}

	return &Client{
		connection: connection,
		reader:     bufio.NewReader(connection),
		writer:     bufio.NewWriter(connection),
	}, nil
}

// Close đóng kết nối TCP tới Tile38.
func (client *Client) Close() error {
	return client.connection.Close()
}

// Do gửi một lệnh RESP tới Tile38 và đọc response tương ứng.
func (client *Client) Do(arguments ...string) (string, error) {
	if len(arguments) == 0 {
		return "", errors.New("Tile38 command has no arguments")
	}

	if err := client.connection.SetDeadline(time.Now().Add(commandTimeout)); err != nil {
		return "", fmt.Errorf("set Tile38 command deadline: %w", err)
	}

	// Tile38 sử dụng RESP. Dòng đầu khai báo số argument của command.
	if _, err := fmt.Fprintf(client.writer, "*%d\r\n", len(arguments)); err != nil {
		return "", fmt.Errorf("write Tile38 command header: %w", err)
	}

	for _, argument := range arguments {
		// Mỗi argument được gửi kèm độ dài byte. Vì vậy tên đường có khoảng
		// trắng hoặc ký tự UTF-8 vẫn được Tile38 đọc thành đúng một argument.
		if _, err := fmt.Fprintf(client.writer, "$%d\r\n%s\r\n", len(argument), argument); err != nil {
			return "", fmt.Errorf("write Tile38 command argument: %w", err)
		}
	}

	// Đẩy toàn bộ command đang nằm trong buffer qua kết nối TCP.
	if err := client.writer.Flush(); err != nil {
		return "", fmt.Errorf("flush Tile38 command: %w", err)
	}

	// Đọc response từ Tile38 và trả về kết quả hoặc lỗi nếu có.
	response, err := readResponse(client.reader)
	if err != nil {
		return "", fmt.Errorf("read Tile38 response: %w", err)
	}

	return response, nil
}

// readResponse đọc các response RESP mà importer cần: simple string, integer,
// error và bulk string.
func readResponse(reader *bufio.Reader) (string, error) {
	prefix, err := reader.ReadByte()
	if err != nil {
		return "", err
	}

	switch prefix {
	case '+', ':':
		// Simple string (+) và integer (:) đều kết thúc bằng CRLF.
		return readLine(reader)
	case '-':
		// Error response (-) phải được chuyển thành Go error để importer dừng.
		message, lineErr := readLine(reader)
		if lineErr != nil {
			return "", lineErr
		}
		return "", errors.New(message)
	case '$':
		// Bulk string ($) khai báo độ dài payload trước nội dung. Tile38 thường
		// dùng dạng này khi trả JSON.
		lengthText, lineErr := readLine(reader)
		if lineErr != nil {
			return "", lineErr
		}
		length, parseErr := strconv.Atoi(lengthText)
		if parseErr != nil {
			return "", fmt.Errorf("invalid bulk response length %q: %w", lengthText, parseErr)
		}
		if length < 0 {
			return "", nil
		}

		payload := make([]byte, length+2)
		if _, readErr := io.ReadFull(reader, payload); readErr != nil {
			return "", readErr
		}
		if string(payload[length:]) != "\r\n" {
			return "", errors.New("invalid bulk response terminator")
		}
		return string(payload[:length]), nil
	default:
		return "", fmt.Errorf("unsupported Tile38 response prefix %q", prefix)
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(line, "\r\n") {
		return "", errors.New("invalid Tile38 response line terminator")
	}
	return strings.TrimSuffix(line, "\r\n"), nil
}
