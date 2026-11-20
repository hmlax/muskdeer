package metadata

import (
	"context"
	"encoding/base64"

	"github.com/go-kratos/kratos/v2/encoding"
	// Register the Kratos JSON codec used by metadata adapters.
	_ "github.com/go-kratos/kratos/v2/encoding/json"
	_ "github.com/go-kratos/kratos/v2/encoding/proto"
	"github.com/go-kratos/kratos/v2/metadata"
	operatorV1 "github.com/hmlax/muskdeer/deer/pb/operator/v1"
)

const (
	mdOperatorKey  = "x-md-global-operator"
	mdSignatureKey = "x-md-global-signature"
)

var codec = encoding.GetCodec("proto")

func NewContext(ctx context.Context, info *operatorV1.OperatorMetadata) (context.Context, error) {
	str, err := encodeOperatorMetadata(info)
	if err != nil {
		return ctx, err
	}
	return metadata.AppendToClientContext(ctx, mdOperatorKey, str), nil
}

func FromContext(ctx context.Context) (*operatorV1.OperatorMetadata, error) {
	md, ok := metadata.FromServerContext(ctx)
	if !ok {
		return nil, ErrNoMetadata
	}

	val := md.Get(mdOperatorKey)
	if val == "" {
		return nil, ErrNoOperatorHeader
	}

	info, err := decodeOperatorMetadata(val)
	if err != nil {
		return nil, ErrInvalidOperator
	}

	return info, nil
}

func encodeOperatorMetadata(info *operatorV1.OperatorMetadata) (string, error) {
	b, err := codec.Marshal(info)
	if err != nil {
		return "", err
	}
	str := base64.RawStdEncoding.EncodeToString(b)
	return str, nil
}

func decodeOperatorMetadata(str string) (*operatorV1.OperatorMetadata, error) {
	b, err := base64.RawStdEncoding.DecodeString(str)
	if err != nil {
		return nil, err
	}

	info := &operatorV1.OperatorMetadata{}
	if err = codec.Unmarshal(b, info); err != nil {
		return nil, err
	}
	return info, nil
}
