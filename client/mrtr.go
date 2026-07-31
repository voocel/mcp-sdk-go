package client

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/mcp-sdk-go/protocol"
)

// InputRequiredError surfaces an MRTR interim result the client could not (or
// was configured not to) fulfill automatically. For manual continuation, set
// InputResponses and RequestState on the original params and call again.
type InputRequiredError struct {
	Requests protocol.InputRequests
	State    string
	Reason   string
}

func (e *InputRequiredError) Error() string {
	return fmt.Sprintf("client: server requires input (%d requests): %s", len(e.Requests), e.Reason)
}

// UnexpectedResultTypeError reports a resultType this client does not handle
// itself. Extensions inspect Raw (e.g. the tasks extension unwraps
// resultType "task" into a CreateTaskResult).
type UnexpectedResultTypeError struct {
	ResultType string
	Raw        json.RawMessage
}

func (e *UnexpectedResultTypeError) Error() string {
	return fmt.Sprintf("client: unexpected resultType %q", e.ResultType)
}

// mrtrFields lets the shared loop write inputResponses/requestState back into
// the concrete params struct before a retry.
type mrtrFields struct {
	responses func(protocol.InputResponses, string)
}

// mrtrCall runs the request-retry loop of the MRTR pattern: fulfill input
// requests, echo requestState byte-exact, retry with a fresh request ID.
func (c *Client) mrtrCall(ctx context.Context, method string, params any, fields mrtrFields, result any) error {
	rounds, shedRetries := 0, 0
	for {
		raw, err := c.do(ctx, method, params)
		if err != nil {
			return err
		}
		rt, err := protocol.PeekResultType(raw)
		if err != nil {
			return err
		}
		switch rt {
		case protocol.ResultTypeComplete:
			return decodeInto(raw, result)
		case protocol.ResultTypeInputRequired:
			var interim protocol.InputRequired
			if err := decodeInto(raw, &interim); err != nil {
				return err
			}
			if c.opts.NoAutoInput {
				return &InputRequiredError{Requests: interim.Requests, State: interim.State, Reason: "automatic fulfillment disabled"}
			}
			rounds++
			if rounds > c.opts.MaxInputRounds {
				return &InputRequiredError{Requests: interim.Requests, State: interim.State,
					Reason: fmt.Sprintf("exceeded %d input rounds", c.opts.MaxInputRounds)}
			}
			if len(interim.Requests) == 0 {
				// Load shedding: retry with state only.
				shedRetries++
				if shedRetries > c.opts.MaxLoadSheddingRetries {
					return &InputRequiredError{State: interim.State,
						Reason: fmt.Sprintf("exceeded %d load-shedding retries", c.opts.MaxLoadSheddingRetries)}
				}
				fields.responses(nil, interim.State)
				continue
			}
			responses, err := c.fulfill(ctx, interim)
			if err != nil {
				return err
			}
			fields.responses(responses, interim.State)
		default:
			return &UnexpectedResultTypeError{ResultType: rt, Raw: raw}
		}
	}
}

// fulfill answers each input request via the configured handlers. Requests
// this client cannot answer (unknown methods, missing handlers) surface as
// *InputRequiredError.
func (c *Client) fulfill(ctx context.Context, interim protocol.InputRequired) (protocol.InputResponses, error) {
	out := protocol.InputResponses{}
	for key, in := range interim.Requests {
		if in.Method != protocol.MethodElicitationCreate {
			return nil, &InputRequiredError{Requests: interim.Requests, State: interim.State,
				Reason: fmt.Sprintf("no handler for input request method %q", in.Method)}
		}
		p, err := in.Elicit()
		if err != nil {
			return nil, err
		}
		var res *protocol.ElicitResult
		switch p.EffectiveMode() {
		case protocol.ElicitModeForm:
			if c.opts.Elicitor == nil {
				return nil, &InputRequiredError{Requests: interim.Requests, State: interim.State,
					Reason: "no Elicitor configured for form-mode elicitation"}
			}
			res, err = c.opts.Elicitor(ctx, p)
			if err != nil {
				return nil, err
			}
		case protocol.ElicitModeURL:
			if c.opts.URLOpener == nil {
				return nil, &InputRequiredError{Requests: interim.Requests, State: interim.State,
					Reason: "no URLOpener configured for url-mode elicitation"}
			}
			if err := c.opts.URLOpener(ctx, p.URL, p.Message); err != nil {
				return nil, err
			}
			res = &protocol.ElicitResult{Action: protocol.ElicitActionAccept}
		default:
			return nil, &InputRequiredError{Requests: interim.Requests, State: interim.State,
				Reason: fmt.Sprintf("unknown elicitation mode %q", p.Mode)}
		}
		if err := out.Set(key, res); err != nil {
			return nil, err
		}
	}
	return out, nil
}
