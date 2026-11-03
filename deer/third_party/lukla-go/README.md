# orderclient (Go SDK)

Go gRPC client for **order-service** (`order-service/proto`).

## Install

```bash
go get gitlab.com/himalaya/lukla-go@latest
```

Or use a local replace in your `go.mod`:

```go
replace gitlab.com/himalaya/lukla-go => ../Himalaya/sdk/go/lukla-go
```

## Regenerate stubs

After changing `.proto` files under `order-service/proto`:

```bash
make -C sdk/go/lukla-go generate
```

## Quick start

```go
package main

import (
	"context"
	"log"
    "os"

	"gitlab.com/himalaya/lukla-go"
	"gitlab.com/himalaya/lukla-go/pb"
)

func main() {
	ctx := context.Background()
	c, err := orderclient.New(ctx, "127.0.0.1:8080", orderclient.WithAuthToken(os.Getenv("SERVICE_AUTH_TOKEN")))
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Ping(ctx); err != nil {
		log.Fatal(err)
	}

	resp, err := c.OpenPositionWith(ctx, orderclient.OpenPositionInput{
		UserID:       1001,
		Symbol:       "BTCUSDT",
		Side:         pb.Side_SIDE_BUY,
		OrderKind:    pb.OrderKind_ORDER_KIND_LIMIT,
		Price:        "95000.00",
		Quantity:     "0.01",
		Leverage:     10,
		PositionType: pb.PositionType_POSITION_TYPE_ISOLATED,
		ClientID:     "my-gateway-req-42",
	})
	if err != nil {
		if code := orderclient.ErrorCodeFrom(err); code != 0 {
			log.Fatalf("business error E%d: %v", code, err)
		}
		log.Fatal(err)
	}
	log.Printf("order_id=%d status=%s client_id=%s", resp.OrderId, resp.Status, resp.ClientId)
}
```

## Multi-node order-service

- The server assigns `order_id` (uint64 snowflake); inputs no longer accept a client order ID.
- Pass `client_id` for your own correlation / idempotency key (stored only in order-service).

Each order-service replica needs a unique `ORDER_SERVICE_WORKER_ID` (0–1023) when generating snowflake ids.

## Streaming RPCs

Use `c.Raw()` for `PlaceStream`, `OpenPositionStream`, `PingStream`, margin streams, etc.

## TLS

```go
import "google.golang.org/grpc/credentials"

creds, _ := credentials.NewClientTLSFromFile("ca.pem", "")
c, err := orderclient.New(ctx, "order-service:8080",
	orderclient.WithDialOptions(grpc.WithTransportCredentials(creds)),
)
```

## Protocol and authentication migration

Regenerate and upgrade all callers together. The current canonical proto field numbers are unchanged;
older generated SDKs used incompatible field numbers and BUY/SELL values. Updated clients send
`x-orderbook-protocol: 2`; authenticated servers reject clients without this version header.
`WithAuthToken` authenticates unary and streaming calls, including `Raw()`.
Use a separate client with `ADMIN_AUTH_TOKEN` for Deposit and TransferIn (including streams).
`fund_id=0` selects the user's default fund; custom funds must first be created by an administrator
credit and ownership cannot be changed by a query or another user.

Run `make sdk-test proto-check` from the repository root to verify generation and wire contracts.

Funding calls require a stable external request ID, e.g.
`admin.TransferIn(ctx, userID, "USDT", "100", "bank-transfer-20261016-001")`.
Reuse the ID on retries; changing the amount or other credit details with the same ID is rejected.

## Funds and transfer retries

Use the `ForFund` variants of GetAccount, Deposit, TransferIn, TransferOut,
ListPositions, ListOrders, ListDeals and ListBills to select a fund explicitly.
Existing convenience methods select the default fund.

All deposits, incoming transfers and withdrawals require an externally stored stable request ID.

`GetAccount.available` is opening margin: wallet balance minus allocated margin, frozen orders and liquidation cash reservations, plus cross-margin unrealized PnL. Isolated unrealized PnL is excluded. Withdrawable cash is `max(0, min(balance, available))`; the server validates this limit. Funding may make `balance` and isolated `extra_margin` negative. Parse monetary strings with exact decimal arithmetic.
Persist it before sending and reuse it on timeout or reconnect. An ID from a successfully committed operation reused with a different
fund, coin, amount or operation is rejected. Requests rejected before commit do not reserve an ID. The SDK never generates an ID during retry.

```go
response, err := client.TransferOutForFund(ctx, userID, fundID, "USDT", "200", "withdrawal-123")
// Default fund: client.TransferOut(ctx, userID, "USDT", "200", "withdrawal-123")
```

Calls to TransferOut without a request ID still compile and return InvalidArgument.
