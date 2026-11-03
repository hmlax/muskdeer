package usdm

import (
	"strings"
	"testing"
	"time"
)

func TestDepthSnapshotPreservesDecimalStringsAndRejectsUnsafeFrames(t *testing.T) {
	now := time.UnixMilli(2000)
	good := `{"e":"depthUpdate","E":1900,"T":1899,"s":"BTCUSDT","U":10,"u":12,"pu":9,"b":[["100.100000000000000001","2.001"]],"a":[["100.2","3"]]}`
	book, err := Decode([]byte(good), "BTCUSDT", now)
	if err != nil || book.Bids[0].Price != "100.100000000000000001" || book.ValidUntilMs != 3400 {
		t.Fatalf("exact complete snapshot lost: %v %v", book, err)
	}
	for _, invalid := range []string{
		strings.Replace(good, `"BTCUSDT"`, `"ETHUSDT"`, 1),
		strings.Replace(good, `"E":1900`, `"E":1`, 1),
		strings.Replace(good, `"E":1900`, `"E":3000`, 1),
		strings.Replace(good, `"2.001"`, `2.001`, 1),
		strings.Replace(good, `"2.001"`, `"0"`, 1),
		strings.Replace(good, `"100.2"`, `"99"`, 1),
		strings.Replace(good, `"u":12`, `"u":0`, 1),
		strings.Replace(good, `"100.2","3"`, `"100.2","3","extra"`, 1),
	} {
		if _, err := Decode([]byte(invalid), "BTCUSDT", now); err == nil {
			t.Fatalf("unsafe source frame accepted: %s", invalid)
		}
	}
}
