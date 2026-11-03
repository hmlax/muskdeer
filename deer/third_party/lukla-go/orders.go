package orderclient

import pb "gitlab.com/himalaya/lukla-go/pb"

// OpenPositionInput builds pb.OpenPositionRequest for OpenPosition / OpenPositionStream.
type OpenPositionInput struct {
	UserID          int64
	FundID          int64
	Symbol          string
	Side            pb.Side
	OrderKind       pb.OrderKind
	Price           string
	Quantity        string
	TimeInForce     pb.TimeInForce
	GtdExpiry       uint64
	Leverage        int32
	PositionType    pb.PositionType
	PositionID      string
	Amount          string
	ClientID        string
	MakerFeeRatePpm int64
	TakerFeeRatePpm int64
}

func (in OpenPositionInput) Proto() *pb.OpenPositionRequest {
	return &pb.OpenPositionRequest{
		UserId:          in.UserID,
		FundId:          in.FundID,
		Symbol:          in.Symbol,
		Side:            in.Side,
		OrderKind:       in.OrderKind,
		Price:           in.Price,
		Quantity:        in.Quantity,
		TimeInForce:     in.TimeInForce,
		GtdExpiry:       in.GtdExpiry,
		Leverage:        in.Leverage,
		PositionType:    in.PositionType,
		PositionId:      in.PositionID,
		Amount:          in.Amount,
		ClientId:        in.ClientID,
		MakerFeeRatePpm: in.MakerFeeRatePpm,
		TakerFeeRatePpm: in.TakerFeeRatePpm,
	}
}

// PlaceInput builds pb.PlaceRequest for Place / PlaceStream.
type PlaceInput struct {
	UserID       int64
	FundID       int64
	Symbol       string
	Side         pb.Side
	OrderKind    pb.OrderKind
	Price        string
	Quantity     string
	TimeInForce  pb.TimeInForce
	GtdExpiry    uint64
	ReduceOnly   bool
	Leverage     int32
	PositionType pb.PositionType
	PositionID   string
	ClientID     string
}

func (in PlaceInput) Proto() *pb.PlaceRequest {
	return &pb.PlaceRequest{
		UserId:       in.UserID,
		FundId:       in.FundID,
		Symbol:       in.Symbol,
		Side:         in.Side,
		OrderKind:    in.OrderKind,
		Price:        in.Price,
		Quantity:     in.Quantity,
		TimeInForce:  in.TimeInForce,
		GtdExpiry:    in.GtdExpiry,
		ReduceOnly:   in.ReduceOnly,
		Leverage:     in.Leverage,
		PositionType: in.PositionType,
		PositionId:   in.PositionID,
		ClientId:     in.ClientID,
	}
}

// ClosePositionInput builds pb.ClosePositionRequest for ClosePosition / ClosePositionStream.
type ClosePositionInput struct {
	UserID      int64
	FundID      int64
	Symbol      string
	OrderKind   pb.OrderKind
	Price       string
	Quantity    string
	TimeInForce pb.TimeInForce
	GtdExpiry   uint64
	PositionID  string
	ClientID    string
}

func (in ClosePositionInput) Proto() *pb.ClosePositionRequest {
	return &pb.ClosePositionRequest{
		UserId:      in.UserID,
		FundId:      in.FundID,
		Symbol:      in.Symbol,
		OrderKind:   in.OrderKind,
		Price:       in.Price,
		Quantity:    in.Quantity,
		TimeInForce: in.TimeInForce,
		GtdExpiry:   in.GtdExpiry,
		PositionId:  in.PositionID,
		ClientId:    in.ClientID,
	}
}

// CancelInput builds pb.CancelOrderRequest.
type CancelInput struct {
	UserID  int64
	FundID  int64
	OrderID uint64
	Symbol  string
}

func (in CancelInput) Proto() *pb.CancelOrderRequest {
	return &pb.CancelOrderRequest{
		UserId:  in.UserID,
		FundId:  in.FundID,
		OrderId: in.OrderID,
		Symbol:  in.Symbol,
	}
}
