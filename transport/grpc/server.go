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

	"github.com/voocel/mcp-sdk-go/protocol"
	pb "github.com/voocel/mcp-sdk-go/transport/grpc/proto"
)

type Handler interface {
	HandleMessage(ctx context.Context, msg *protocol.JSONRPCMessage) (*protocol.JSONRPCMessage, error)
}

type MCPServiceServer struct {
	pb.UnimplementedMCPServiceServer
	handler Handler
}

func (s *MCPServiceServer) ProcessMessage(ctx context.Context, req *pb.Request) (*pb.Response, error) {
	msg := &protocol.JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      json.RawMessage(fmt.Sprintf(`"%s"`, req.Id)),
		Method:  req.Method,
	}

	if req.Params != nil {
		msg.Params = req.Params
	}

	response, err := s.handler.HandleMessage(ctx, msg)
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

	if response == nil {
		return &pb.Response{
			Id:        req.Id,
			Timestamp: time.Now().UnixNano(),
		}, nil
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	var responseMap map[string]interface{}
	if err := json.Unmarshal(responseData, &responseMap); err != nil {
		return nil, fmt.Errorf("invalid response format: %w", err)
	}

	var idStr string
	if id, ok := responseMap["id"]; ok {
		idStr = fmt.Sprintf("%v", id)
	} else {
		idStr = req.Id
	}

	resp := &pb.Response{
		Id:        idStr,
		Timestamp: time.Now().UnixNano(),
	}

	if errObj, ok := responseMap["error"]; ok {
		errMap := errObj.(map[string]interface{})
		resp.Error = &pb.Error{
			Code:    int32(errMap["code"].(float64)),
			Message: errMap["message"].(string),
		}
		if data, ok := errMap["data"]; ok {
			dataBytes, _ := json.Marshal(data)
			resp.Error.Data = dataBytes
		}
	} else if result, ok := responseMap["result"]; ok {
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
	handler    Handler
	grpcServer *grpc.Server
	addr       string
	mu         sync.Mutex
	started    bool
}

func NewServer(addr string, handler Handler) *Server {
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
