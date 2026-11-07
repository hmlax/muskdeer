package native

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"google.golang.org/protobuf/proto"

	commonv1 "github.com/hmlax/muskdeer/deer/pb/common/v1"
)

// SyntheticPolicyDigest preserves the identity of migrated consumed slots.
// Instance loading separately binds that historical identity to current parameters.
func SyntheticPolicyDigest(policy *commonv1.VenuePolicy) (string, error) {
	if policy == nil {
		return "", ErrInvalid
	}
	if policy.SyntheticPolicyDigest != "" {
		value, err := hex.DecodeString(policy.SyntheticPolicyDigest)
		if err != nil || len(value) != sha256.Size || hex.EncodeToString(value) != policy.SyntheticPolicyDigest {
			return "", ErrInvalid
		}
		return policy.SyntheticPolicyDigest, nil
	}
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(policy)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}
