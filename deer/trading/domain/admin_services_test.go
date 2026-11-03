package domain

import "testing"

func TestAdminServiceFilterRejectsArbitraryTargetsAndUnboundedPages(t *testing.T) {
	for _, role := range []AdminServiceRole{AdminServiceAPI, AdminServiceMarket, AdminServiceStrategy, AdminServiceExecution, AdminServicePortfolio, AdminServiceCron} {
		if err := (AdminServiceFilter{
			Role:  role,
			Limit: MaxAdminServicePage,
		}).Validate(); err != nil {
			t.Fatal("fixed platform role rejected", err)
		}
	}
	for _, filter := range []AdminServiceFilter{
		{
			Role:  "http://127.0.0.1:9000",
			Limit: 1,
		},
		{
			Role:  AdminServiceStrategy,
			Limit: MaxAdminServicePage + 1,
		},
		{
			Role:  AdminServiceStrategy,
			Limit: 0,
		},
		{
			Role:            AdminServiceStrategy,
			Limit:           1,
			AfterInstanceID: " instance",
		},
		{
			Role:            AdminServiceStrategy,
			Limit:           1,
			AfterInstanceID: "bad\x00instance",
		},
	} {
		if filter.Validate() == nil {
			t.Fatal("invalid service filter accepted")
		}
	}
}
