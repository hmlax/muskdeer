package marketwire

import (
	"errors"
	"testing"
	"time"
)

func TestOKXControlDoesNotCreateBookAndRejectsAmbiguousVariants(t *testing.T) {
	key := Instrument{
		ID:     1,
		Symbol: "BTC-USDT",
	}
	for _, data := range []string{
		`pong`,
		`{"event":"subscribe","arg":{"channel":"books","instId":"BTC-USDT"},"connId":"connection"}`,
	} {
		packet, err := OKXPacket(t.Context(), []byte(data), key, time.Now())
		if err != nil || packet.Kind == PacketDepth || packet.Frame.Sequence != 0 || !packet.Frame.ReceivedAt.IsZero() {
			t.Fatalf("control created market data: %+v, %v", packet, err)
		}
	}
	for _, data := range []string{
		`{"event":"subscribe","arg":{"channel":"books","instId":"ETH-USDT"},"connId":"connection"}`,
		`{"event":"subscribe","arg":{"channel":"books","instId":"BTC-USDT"},"connId":"connection","data":[]}`,
		`{"event":"subscribe","event":"error"}`,
		`{"Event":"subscribe"}`,
		`{"event":"notice","code":"other"}`,
		`{"event":"subscribe","arg":{"channel":"books","instId":"BTC-USDT"}}`,
	} {
		if _, err := OKXPacket(t.Context(), []byte(data), key, time.Now()); !errors.Is(err, ErrInvalidPayload) {
			t.Fatalf("ambiguous control accepted: %v", err)
		}
	}
	if _, err := OKXPacket(t.Context(), []byte(`{"event":"error","code":"60012","msg":"untrusted secret"}`), key, time.Now()); !errors.Is(err, ErrSubscription) || err.Error() != MessageSubscription {
		t.Fatalf("unsafe subscription error: %v", err)
	}
}
