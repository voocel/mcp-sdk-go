package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/voocel/mcp-sdk-go/transport"
	pb "github.com/voocel/mcp-sdk-go/transport/grpc/proto"
)

type MCPServiceServer struct {
	pb.UnimplementedMCPServiceServer
	handler transport.Handler
}

func (s *MCPServiceServer) ProcessMessage(ctx context.Context, req *pb.Request) (*pb.Response, error) {
	message := map[string]interface{}{
		"id":        req.Id,
		"method":    req.Method,
		"timestamp": time.Unix(0, req.Timestamp),
	}

	if req.Params != nil {
		var params interface{}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return nil, fmt.Errorf("invalid parameters: %w", err)
		}
		message["params"] = params
	}

	data, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	responseData, err := s.handler.HandleMessage(ctx, data)
	if err != nil {
		return &pb.Response{
			Id:        req.Id,
			Timestamp: time.Now().UnixNano(),
			Error: &pb.Error{
				Code:    -1,
				Message: err.Error(),
			},
		}, nil
	}

	var response map[string]interface{}
	if err := json.Unmarshal(responseData, &response); err != nil {
		return nil, fmt.Errorf("invalid response format: %w", err)
	}

	resp := &pb.Response{
		Id:        response["id"].(string),
		Timestamp: time.Now().UnixNano(),
	}

	if errObj, ok := response["error"]; ok {
		errMap := errObj.(map[string]interface{})
		resp.Error = &pb.Error{
			Code:    int32(errMap["code"].(float64)),
			Message: errMap["message"].(string),
		}
		if data, ok := errMap["data"]; ok {
			dataBytes, _ := json.Marshal(data)
			resp.Error.Data = dataBytes
		}
	} else if result, ok := response["result"]; ok {
		resultBytes, _ := json.Marshal(result)
		resp.Result = resultBytes
	}

	return resp, nil
}

func (s *MCPServiceServer) StreamMessages(stream pb.MCPService_StreamMessagesServer) error {
	ctx := stream.Context()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			req, err := stream.Recv()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}

			resp, err := s.ProcessMessage(ctx, req)
			if err != nil {
				return err
			}
			if err := stream.Send(resp); err != nil {
				return err
			}
		}
	}
}

type Server struct {
	handler    transport.Handler
	grpcServer *grpc.Server
	addr       string
	mu         sync.Mutex
	started    bool
}

func NewServer(addr string, handler transport.Handler) *Server {
	return &Server{
		handler: handler,
		addr:    addr,
	}
}

func (s *Server) Serve(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("server already started")
	}

	s.grpcServer = grpc.NewServer()
	s.mu.Unlock()

	mcpServer := &MCPServiceServer{handler: s.handler}
	pb.RegisterMCPServiceServer(s.grpcServer, mcpServer)

	reflection.Register(s.grpcServer)

	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	go func() {
		<-ctx.Done()
		s.Shutdown(context.Background())
	}()

	s.mu.Lock()
	s.started = true
	s.mu.Unlock()

	if err := s.grpcServer.Serve(lis); err != nil && err != grpc.ErrServerStopped {
		return err
	}

	return nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}

	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
		s.started = false
	}

	return nil
}
