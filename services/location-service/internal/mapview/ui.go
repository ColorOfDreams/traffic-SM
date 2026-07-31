package mapview

import _ "embed"

//go:embed web/map.html
var mapPage []byte

func Page() []byte {
	return append([]byte(nil), mapPage...)
}
