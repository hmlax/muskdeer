package main

import (
	"github.com/hmlax/muskdeer/admin/biz/handler"
	"github.com/hmlax/muskdeer/admin/biz/router"
	"github.com/hmlax/muskdeer/admin/internal/transport"
)

// customizedRegister mounts non-business routes; business routes belong in IDL.
func customizedRegister(h *transport.Router, services *handler.Services) {
	if services.EnableSwagger {
		router.RegisterSwagger(h)
	}
}
