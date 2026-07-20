package engine

import "strings"

// placeholder is one `{{ expression }}` occurrence found by scanPlaceholders,
// with byte offsets into the original string.
type placeholder struct {
	start, end int
	expr       string
}

// scanPlaceholders finds every `{{ ... }}` occurrence in s (spec §9),
// honouring quoted literals when looking for the closing `}}` (spec §9.5): a
// `}}` inside a single- or double-quoted literal never terminates the
// placeholder.
func scanPlaceholders(s string) []placeholder {
	var out []placeholder
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '{' && s[i+1] == '{' {
			j := i + 2
			var quote byte
			for j < len(s) {
				c := s[j]
				if quote != 0 {
					if c == quote {
						quote = 0
					}
					j++
					continue
				}
				if c == '\'' || c == '"' {
					quote = c
					j++
					continue
				}
				if c == '}' && j+1 < len(s) && s[j+1] == '}' {
					break
				}
				j++
			}
			if j+1 < len(s) && s[j] == '}' && s[j+1] == '}' {
				out = append(out, placeholder{start: i, end: j + 2, expr: strings.TrimSpace(s[i+2 : j])})
				i = j + 2
				continue
			}
			i += 2
			continue
		}
		i++
	}
	return out
}

func isQuotedLiteral(expr string) bool {
	if len(expr) < 2 {
		return false
	}
	q := expr[0]
	return (q == '\'' || q == '"') && expr[len(expr)-1] == q
}

// renderContext carries the request a templated response is being rendered
// against (spec §9.1).
type renderContext struct {
	req *Request
}

// evalPlaceholder resolves one placeholder expression against ctx, per the
// namespaces of spec §9.1. It returns nil for an unresolved reference,
// exactly like a null target (spec §4.5): both render as the empty string
// in running text and as "" in a typed whole-value position (spec §9.4).
func evalPlaceholder(expr string, ctx *renderContext) any {
	if isQuotedLiteral(expr) {
		return expr[1 : len(expr)-1]
	}
	req := ctx.req
	switch {
	case strings.HasPrefix(expr, "params."):
		name := expr[len("params."):]
		if v, ok := req.Params[name]; ok {
			return v
		}
		return nil
	case expr == "request.body":
		if req.Protocol == "http" {
			return req.Body
		}
		return nil
	case strings.HasPrefix(expr, "request.body."):
		if req.Protocol != "http" {
			return nil
		}
		v, _ := resolveDottedPath(req.Body, expr[len("request.body."):])
		return v
	case expr == "request.message":
		if req.Protocol == "grpc" || req.Protocol == "websocket" {
			return req.Message
		}
		return nil
	case strings.HasPrefix(expr, "request.message."):
		if req.Protocol != "grpc" && req.Protocol != "websocket" {
			return nil
		}
		v, _ := resolveDottedPath(req.Message, expr[len("request.message."):])
		return v
	case strings.HasPrefix(expr, "request.headers."):
		name := strings.ToLower(expr[len("request.headers."):])
		if v, ok := req.Headers[name]; ok {
			return v
		}
		return nil
	case strings.HasPrefix(expr, "request.query."):
		name := expr[len("request.query."):]
		if vals, ok := req.Query[name]; ok && len(vals) == 1 {
			return vals[0]
		}
		return nil
	case strings.HasPrefix(expr, "request.metadata."):
		if req.Protocol != "grpc" {
			return nil
		}
		name := strings.ToLower(expr[len("request.metadata."):])
		if v, ok := req.Metadata[name]; ok {
			return v
		}
		return nil
	case strings.HasPrefix(expr, "request.variables."):
		if req.Protocol != "graphql" {
			return nil
		}
		v, _ := resolveDottedPath(req.Variables, expr[len("request.variables."):])
		return v
	case strings.HasPrefix(expr, "faker."):
		rest := expr[len("faker."):]
		parts := strings.SplitN(rest, ".", 2)
		if len(parts) == 2 && knownFaker(parts[0], parts[1]) {
			return fakerValue(parts[0], parts[1])
		}
		return nil
	}
	return nil
}

// renderMixedText replaces every placeholder in s with its resolved value's
// canonical string form (or the empty string when it has none), the rule
// that governs both running text and every wire-string position, whole
// value or not (spec §9.2, §9.4).
func renderMixedText(s string, ctx *renderContext) string {
	phs := scanPlaceholders(s)
	if len(phs) == 0 {
		return s
	}
	var sb strings.Builder
	last := 0
	for _, ph := range phs {
		sb.WriteString(s[last:ph.start])
		if str, ok := canonicalString(evalPlaceholder(ph.expr, ctx)); ok {
			sb.WriteString(str)
		}
		last = ph.end
	}
	sb.WriteString(s[last:])
	return sb.String()
}

// renderWireString renders a wire-string position (HTTP headers, gRPC
// metadata/trailers/error, WebSocket close.reason): always a string, even
// for a whole-value placeholder (spec §9.4).
func renderWireString(s string, ctx *renderContext) string {
	return renderMixedText(s, ctx)
}

// renderNativeValue renders a native-payload position (HTTP body; gRPC
// message/messages; GraphQL data/errors/extensions; WebSocket messages). A
// value consisting of exactly one placeholder injects that placeholder's
// native type; anything else renders as a string (spec §9.4).
func renderNativeValue(s string, ctx *renderContext) any {
	phs := scanPlaceholders(s)
	if len(phs) == 1 && phs[0].start == 0 && phs[0].end == len(s) {
		v := evalPlaceholder(phs[0].expr, ctx)
		if v == nil {
			return ""
		}
		return v
	}
	return renderMixedText(s, ctx)
}

// renderTree walks a parsed template value (built by nodeToValue), rendering
// every string leaf. wire selects wire-string vs native-payload rendering
// for those leaves; maps and lists are always native structure.
func renderTree(v any, ctx *renderContext, wire bool) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = renderTree(val, ctx, wire)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = renderTree(val, ctx, wire)
		}
		return out
	case string:
		if wire {
			return renderWireString(t, ctx)
		}
		return renderNativeValue(t, ctx)
	default:
		return t
	}
}

// render renders a matched scenario's response (spec §8 step 5, §9) into the
// normalized-response shape of conformance/README.md's expected.json.
func render(server *Server, op *Operation, resp *Response, req *Request) map[string]any {
	ctx := &renderContext{req: req}
	out := map[string]any{}
	if resp.Delay != 0 {
		out["delay"] = float64(resp.Delay)
	}
	switch server.Type {
	case ServerHTTP:
		out["status"] = float64(resp.Status)
		if len(resp.Headers) > 0 {
			out["headers"] = renderHeaders(resp.Headers, ctx)
		}
		if resp.Body != nil {
			out["body"] = renderTree(resp.Body, ctx, false)
		}
	case ServerGRPC:
		out["status"] = resp.GRPCStatus
		if resp.Error != "" {
			out["error"] = renderWireString(resp.Error, ctx)
		}
		if len(resp.Metadata) > 0 {
			out["metadata"] = renderStringMap(resp.Metadata, ctx)
		}
		if len(resp.Trailers) > 0 {
			out["trailers"] = renderStringMap(resp.Trailers, ctx)
		}
		if resp.messageSet {
			out["message"] = renderTree(resp.Message, ctx, false)
		}
		if resp.messagesSet {
			msgs := make([]any, len(resp.Messages))
			for i, m := range resp.Messages {
				msgs[i] = renderTree(m, ctx, false)
			}
			out["messages"] = msgs
		}
	case ServerGraphQL:
		if resp.dataSet {
			out["data"] = renderTree(resp.Data, ctx, false)
		}
		if len(resp.Errors) > 0 {
			errs := make([]any, len(resp.Errors))
			for i, e := range resp.Errors {
				errs[i] = renderTree(e, ctx, false)
			}
			out["errors"] = errs
		}
		if resp.Extensions != nil {
			out["extensions"] = renderTree(resp.Extensions, ctx, false)
		}
	case ServerWebSocket:
		if resp.wsMessagesSet {
			msgs := make([]any, len(resp.WSMessages))
			for i, m := range resp.WSMessages {
				msgs[i] = renderTree(m, ctx, false)
			}
			out["messages"] = msgs
		}
		if resp.Close != nil {
			out["close"] = map[string]any{
				"code":   float64(resp.Close.Code),
				"reason": renderWireString(resp.Close.Reason, ctx),
			}
		}
	}
	return out
}

func renderHeaders(headers map[string]HeaderTemplate, ctx *renderContext) map[string]any {
	out := make(map[string]any, len(headers))
	for k, h := range headers {
		if h.IsList {
			list := make([]any, len(h.Values))
			for i, v := range h.Values {
				list[i] = renderWireString(v, ctx)
			}
			out[k] = list
			continue
		}
		out[k] = renderWireString(h.Values[0], ctx)
	}
	return out
}

func renderStringMap(m map[string]string, ctx *renderContext) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = renderWireString(v, ctx)
	}
	return out
}
