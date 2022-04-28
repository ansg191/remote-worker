package main

import (
	"context"
	"flag"
	"fmt"
	"net"

	"github.com/aws/aws-sdk-go-v2/config"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/ansg191/remote-worker/api/proto"
)

var (
	port     = flag.Int("p", 8000, "Port to listen on")
	tempPath = flag.String("tmp", "/tmp", "Temporary File Path")
)

func main() {
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer func(logger *zap.Logger) {
		_ = logger.Sync()
	}(logger)

	logger.Debug("Configuration Info",
		zap.Stringp("tempPath", tempPath),
	)

	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", *port))
	if err != nil {
		logger.Fatal("listener error", zap.Error(err))
	}

	logger.Debug("Listener running", zap.Intp("port", port))

	grpcServer := grpc.NewServer()

	workerServer := &WorkerServer{logger: logger}
	proto.RegisterWorkerServiceServer(grpcServer, workerServer)

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		logger.Fatal("AWS config error", zap.Error(err))
	}

	jobServer := &JobServer{
		logger:   logger,
		cfg:      cfg,
		tempPath: *tempPath,
	}
	proto.RegisterJobServiceServer(grpcServer, jobServer)

	logger.Info("Server running", zap.Intp("port", port))

	err = grpcServer.Serve(lis)
	if err != nil {
		logger.Fatal("grpc listener failure", zap.Error(err))
	}
}
