package main

import (
	"context"

	"go.uber.org/zap"

	"golang.anshulg.com/popcorntime/go_encoder/api/proto"
)

type WorkerServer struct {
	proto.UnimplementedWorkerServiceServer
	logger *zap.Logger
}

func (w *WorkerServer) Status(ctx context.Context, request *proto.WorkerStatusRequest) (*proto.WorkerStatusResponse, error) {
	return &proto.WorkerStatusResponse{Msg: "OK"}, nil
}
