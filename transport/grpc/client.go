package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/voocel/mcp-sdk-go/transport/grpc/proto"
)

type Transport struct {
	target     string
	conn       *grpc.ClientConn
	client     pb.MCPServiceClient
	stream     pb.MCPService_StreamMessagesClient
	messageCh  chan []byte
	closeCh    chan struct{}
	closedOnce sync.Once
	mu         sync.Mutex
}

type Option func(*Transport)

func WithInsecure() Option {
	return func(t *Transport) {}
}

func New(target string, options ...Option) *Transport {
	t := &Transport{
		target:    target,
		messageCh: make(chan []byte, 100),
		closeCh:   make(chan struct{}),
	}

	for _, option := range options {
		option(t)
	}

	return t
}

func (t *Transport) Connect(ctx context.Context) error {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	}

	conn, err := grpc.DialContext(ctx, t.target, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	t.conn = conn
	t.client = pb.NewMCPServiceClient(conn)

	stream, err := t.client.StreamMessages(ctx)
	if err != nil {
		t.conn.Close()
		return fmt.Errorf("failed to create stream: %w", err)
	}

	t.stream = stream

	go t.receiveLoop(ctx)

	return nil
}

func (t *Transport) receiveLoop(ctx context.Context) {
	defer close(t.messageCh)

	for {
		select {
		case <-t.closeCh:
			return
		case <-ctx.Done():
			return
		default:
			resp, err := t.stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}

			response := map[string]interface{}{
				"id":        resp.Id,
				"timestamp": time.Unix(0, resp.Timestamp),
			}

			if resp.Error != nil {
				response["error"] = map[string]interface{}{
					"code":    resp.Error.Code,
					"message": resp.Error.Message,
				}
				if resp.Error.Data != nil {
					response["error"].(map[string]interface{})["data"] = resp.Error.Data
				}
			} else {
				response["result"] = resp.Result
			}

			jsonData, err := json.Marshal(response)
			if err != nil {
				continue
			}

			select {
			case t.messageCh <- jsonData:
			case <-t.closeCh:
				return
			case <-ctx.Done():
				return
			}
		}
	}
}

func (t *Transport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.stream == nil {
		return fmt.Errorf("gRPC stream not established")
	}

	var message map[string]interface{}
	if err := json.Unmarshal(data, &message); err != nil {
		return fmt.Errorf("invalid message format: %w", err)
	}

	req := &pb.Request{
		Id:        message["id"].(string),
		Method:    message["method"].(string),
		Timestamp: time.Now().UnixNano(),
	}

	if params, ok := message["params"]; ok {
		paramsBytes, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal parameters: %w", err)
		}
		req.Params = paramsBytes
	}

	if err := t.stream.Send(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	return nil
}

func (t *Transport) Receive(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-t.messageCh:
		if !ok {
			return nil, fmt.Errorf("connection closed")
		}
		return msg, nil
	}
}

func (t *Transport) Close() error {
	var err error

	t.closedOnce.Do(func() {
		close(t.closeCh)

		t.mu.Lock()
		if t.stream != nil {
			t.stream.CloseSend()
		}
		if t.conn != nil {
			err = t.conn.Close()
		}
		t.mu.Unlock()
	})

	return err
}
