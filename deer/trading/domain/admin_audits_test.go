package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestAdminAuditFilterBoundsExactTextWithoutPatternInterpretation(t *testing.T) {
	for _, value := range []string{"", "strategy_instance:9007199254740993", "精确对象", "%_';--"} {
		if err := (AdminAuditFilter{
			Page:     AdminPage{Limit: MaxAdminPageSize},
			Resource: value,
		}).Validate(); err != nil {
			t.Fatalf("bounded literal filter was rejected: %v", err)
		}
	}
	for _, filter := range []AdminAuditFilter{
		{Page: AdminPage{Limit: 0}},
		{Page: AdminPage{Limit: MaxAdminPageSize + 1}},
		{Page: AdminPage{
			AfterID: -1,
			Limit:   1,
		}},
		{
			Page:  AdminPage{Limit: 1},
			Actor: strings.Repeat("a", 65),
		},
		{
			Page:   AdminPage{Limit: 1},
			Action: strings.Repeat("a", 65),
		},
		{
			Page:     AdminPage{Limit: 1},
			Resource: strings.Repeat("界", 43),
		},
		{
			Page:  AdminPage{Limit: 1},
			Actor: " actor",
		},
		{
			Page:   AdminPage{Limit: 1},
			Action: "action ",
		},
		{
			Page:     AdminPage{Limit: 1},
			Resource: "line\nfeed",
		},
		{
			Page:     AdminPage{Limit: 1},
			Resource: "nul\x00byte",
		},
		{
			Page:     AdminPage{Limit: 1},
			Resource: string([]byte{0xff}),
		},
	} {
		if err := filter.Validate(); !errors.Is(err, ErrInvalidAdminQuery) {
			t.Fatalf("invalid audit filter was accepted: %#v, %v", filter, err)
		}
	}
}
