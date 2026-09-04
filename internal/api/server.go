package api

import (
	"github.com/0x3ea/bigbrother/internal/config"
	"github.com/0x3ea/bigbrother/internal/store"
	"github.com/gin-gonic/gin"
)

type Server struct {
	store   store.Store
	targets []config.Target
}

func New(s store.Store, targets []config.Target) *Server {
	return &Server{store: s, targets: targets}
}

func (s *Server) Engine() *gin.Engine {
	r := gin.Default()
	v1 := r.Group("/api/v1")
	v1.GET("/status", s.status)
	return r
}
