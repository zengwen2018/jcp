// Package webapi 提供将 App 绑定方法暴露为 HTTP JSON API 的能力。
//
// 设计要点：
//   - 用反射统一适配，避免为每个方法手写 handler；
//   - 路由表显式声明路径与参数名，前端调用完全可预测；
//   - 标量参数走查询字符串，结构体参数走 JSON body；
//   - 返回值统一包装为 { "data": ... } 或 { "error": ... }。
package webapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
)

// Router 是轻量路由器，负责注册与分发 HTTP 路由。
//
// 选用标准库 net/http 而非第三方框架，是为了让 Web 模式
// 的二进制依赖保持在最小集合内（轻量服务器内存有限）。
//
// 同一路径可以同时承载 GET 与 POST（如 GET /api/config 读配置、
// POST /api/config 写配置），内部按方法分派到不同的处理器。
type Router struct {
	mux         *http.ServeMux
	middlewares []func(http.Handler) http.Handler
	// methodHandlers 记录每个路径对应的方法处理器。
	methodHandlers map[string]map[string]http.HandlerFunc
	// pendingPatterns 记录待注册到 mux 的新路径。
	pendingPatterns []string
}

// NewRouter 创建路由。
func NewRouter() *Router {
	return &Router{
		mux:            http.NewServeMux(),
		methodHandlers: make(map[string]map[string]http.HandlerFunc),
	}
}

// Use 注册全局中间件，按注册顺序执行。
func (r *Router) Use(mw ...func(http.Handler) http.Handler) {
	r.middlewares = append(r.middlewares, mw...)
}

// Handle 注册处理函数。
func (r *Router) Handle(pattern string, h http.HandlerFunc) {
	r.mux.Handle(pattern, h)
}

// HandleFunc 注册处理函数。
func (r *Router) HandleFunc(pattern string, h http.HandlerFunc) {
	r.mux.HandleFunc(pattern, h)
}

// HandleMethod 注册按 HTTP 方法分派的处理函数。
//
// 同一路径注册多个方法时不会冲突——这正是标准库 ServeMux
// 无法原生支持、而本项目需要的语义（读/写共用路径）。
func (r *Router) HandleMethod(pattern, method string, h http.HandlerFunc) {
	handlers, ok := r.methodHandlers[pattern]
	if !ok {
		handlers = make(map[string]http.HandlerFunc)
		r.methodHandlers[pattern] = handlers
		// 新路径：记录需要注册 mux 入口。
		r.pendingPatterns = append(r.pendingPatterns, pattern)
	}
	handlers[method] = h

	// 每个路径只向 mux 注册一次入口。用 pendingPatterns 记录新路径，
	// 避免"同路径同方法重复注册"时误判为首次。
	if len(r.pendingPatterns) > 0 && r.pendingPatterns[len(r.pendingPatterns)-1] == pattern {
		r.pendingPatterns = r.pendingPatterns[:len(r.pendingPatterns)-1]
		r.mux.HandleFunc(pattern, func(w http.ResponseWriter, req *http.Request) {
			hs := r.methodHandlers[pattern]
			if handler, ok := hs[req.Method]; ok {
				handler(w, req)
				return
			}
			// OPTIONS 预检直接放行。
			if req.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			writeError(w, http.StatusMethodNotAllowed,
				"该路径不支持 "+req.Method+" 方法")
		})
	}
}

// ServeHTTP 实现 http.Handler，串联中间件。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	var h http.Handler = r.mux
	// 中间件按注册顺序包裹：先注册的在最外层。
	for i := len(r.middlewares) - 1; i >= 0; i-- {
		h = r.middlewares[i](h)
	}
	h.ServeHTTP(w, req)
}

// ArgKind 描述单个参数的来源。
type ArgKind int

const (
	// ArgQuery 表示参数取自查询字符串。
	ArgQuery ArgKind = iota
	// ArgBody 表示参数取自 JSON body。
	ArgBody
)

// Arg 描述方法的一个参数。
type Arg struct {
	// Kind 参数来源。
	Kind ArgKind
	// Name 查询参数名（Kind 为 ArgQuery 时必填）。
	Name string
}

// Q 定义一个查询字符串参数。
func Q(name string) Arg { return Arg{Kind: ArgQuery, Name: name} }

// B 定义一个 JSON body 参数。
func B() Arg { return Arg{Kind: ArgBody} }

// Route 描述一条 HTTP 路由与 App 方法的映射。
type Route struct {
	// Method App 上的方法名，如 "GetKLineData"。
	Method string
	// Path HTTP 路径，如 "/api/stock/kline"。
	Path string
	// Args 参数描述，顺序须与方法签名一致。
	Args []Arg
	// HTTPMethod 限定 HTTP 方法，留空表示 GET（存在 body 参数时自动为 POST）。
	HTTPMethod string
}

// binder 将 App 的方法注册为 HTTP 路由。
type binder struct {
	router   *Router
	receiver reflect.Value
	// sanitizeResponse 在返回前对响应数据做脱敏（如隐藏 API Key）。
	sanitizeResponse func(data any) any
	// sanitizeRequest 在调用前把脱敏过的请求体还原为完整明文。
	sanitizeRequest func(body []byte) []byte
}

// NewBinder 创建绑定器。
func NewBinder(router *Router, receiver any) *binder {
	return &binder{router: router, receiver: reflect.ValueOf(receiver)}
}

// WithSanitizer 配置请求/响应的脱敏钩子。
func (b *binder) WithSanitizer(
	sanitizeResponse func(data any) any,
	sanitizeRequest func(body []byte) []byte,
) *binder {
	b.sanitizeResponse = sanitizeResponse
	b.sanitizeRequest = sanitizeRequest
	return b
}

// BindAll 按路由表批量注册。
func (b *binder) BindAll(routes []Route) error {
	for _, rt := range routes {
		if err := b.Bind(rt); err != nil {
			return err
		}
	}
	return nil
}

// Bind 注册单个路由。
func (b *binder) Bind(rt Route) error {
	fn := b.receiver.MethodByName(rt.Method)
	if !fn.IsValid() {
		return fmt.Errorf("绑定 %s 失败：App 上不存在该方法", rt.Method)
	}
	// MethodByName 返回的是 bound method，receiver 已从参数列表中剥离，
	// 因此 NumIn() 就是业务参数个数。
	ft := fn.Type()
	argCount := ft.NumIn()
	if argCount != len(rt.Args) {
		return fmt.Errorf("绑定 %s 失败：参数数量不匹配，方法需要 %d 个，路由表声明 %d 个",
			rt.Method, argCount, len(rt.Args))
	}

	httpMethod := rt.HTTPMethod
	if httpMethod == "" {
		httpMethod = http.MethodGet
		for _, a := range rt.Args {
			if a.Kind == ArgBody {
				httpMethod = http.MethodPost
				break
			}
		}
	}

	entry := &boundRoute{
		fn:    fn,
		route: rt,
		ft:    ft,
	}

	b.router.HandleMethod(rt.Path, httpMethod, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		b.handle(entry, w, r)
	})
	return nil
}

// boundRoute 保存已解析的路由信息。
type boundRoute struct {
	fn    reflect.Value
	route Route
	ft    reflect.Type
}

// handle 处理一次请求。
func (b *binder) handle(e *boundRoute, w http.ResponseWriter, r *http.Request) {
	args, err := b.parseArgs(r, e)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// bound method 的 Call 不需要传 receiver。
	in := args

	out := e.fn.Call(in)

	// 若最后一个返回值是 error，优先处理。
	if len(out) > 0 {
		last := out[len(out)-1]
		if last.Type() == reflect.TypeOf((*error)(nil)).Elem() {
			if !last.IsNil() {
				writeError(w, http.StatusInternalServerError, last.Interface().(error).Error())
				return
			}
			out = out[:len(out)-1]
		}
	}

	// 无返回值的方法（如 NotifyFrontendReady）返回空数据。
	if len(out) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"data": nil})
		return
	}
	data := out[0].Interface()
	if b.sanitizeResponse != nil {
		data = b.sanitizeResponse(data)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

// parseArgs 按路由表解析方法参数。
func (b *binder) parseArgs(r *http.Request, e *boundRoute) ([]reflect.Value, error) {
	argCount := e.ft.NumIn()
	args := make([]reflect.Value, argCount)
	query := r.URL.Query()

	// body 惰性解析：仅在有 body 参数且请求体非空时读取。
	var bodyData []byte
	bodyLoaded := false
	loadBody := func() ([]byte, error) {
		if !bodyLoaded {
			b, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, err
			}
			bodyData = b
			bodyLoaded = true
		}
		return bodyData, nil
	}

	for i := 0; i < argCount; i++ {
		spec := e.route.Args[i]
		t := e.ft.In(i)

		switch spec.Kind {
		case ArgBody:
			raw, err := loadBody()
			if err != nil {
				return nil, fmt.Errorf("读取请求体失败: %w", err)
			}
			// 指针类型参数需要构造两层：*T -> **T -> *T
			baseType := t
			isPtr := baseType.Kind() == reflect.Ptr
			if isPtr {
				baseType = baseType.Elem()
			}
			if len(strings.TrimSpace(string(raw))) == 0 {
				if isPtr {
					args[i] = reflect.Zero(t)
				} else {
					args[i] = reflect.New(baseType).Elem()
				}
				continue
			}
			// 还原被脱敏的敏感字段（如 API Key）。
			if b.sanitizeRequest != nil {
				raw = b.sanitizeRequest(raw)
			}
			ptr := reflect.New(baseType)
			if err := json.Unmarshal(raw, ptr.Interface()); err != nil {
				return nil, fmt.Errorf("请求体解析失败: %w", err)
			}
			if isPtr {
				args[i] = ptr
			} else {
				args[i] = ptr.Elem()
			}

		case ArgQuery:
			raw := query.Get(spec.Name)

			// 切片参数用逗号分隔，如 ?codes=sh600000,sz000001
			if t.Kind() == reflect.Slice && t.Elem().Kind() != reflect.Struct {
				parts := []string{}
				if raw != "" {
					parts = strings.Split(raw, ",")
				}
				slice := reflect.MakeSlice(t, 0, len(parts))
				for _, p := range parts {
					p = strings.TrimSpace(p)
					if p == "" {
						continue
					}
					elem, err := convertScalar(p, t.Elem())
					if err != nil {
						return nil, fmt.Errorf("参数 %s 无效: %w", spec.Name, err)
					}
					slice = reflect.Append(slice, elem)
				}
				args[i] = slice
				continue
			}

			val, err := convertScalar(raw, t)
			if err != nil {
				return nil, fmt.Errorf("参数 %s 无效: %w", spec.Name, err)
			}
			args[i] = val
		}
	}
	return args, nil
}

// convertScalar 将字符串转换为目标标量类型。
func convertScalar(raw string, t reflect.Type) (reflect.Value, error) {
	v := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if raw == "" {
			return v, nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return v, err
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if raw == "" {
			return v, nil
		}
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return v, err
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		if raw == "" {
			return v, nil
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return v, err
		}
		v.SetFloat(f)
	case reflect.Bool:
		if raw == "" {
			return v, nil
		}
		bv, err := strconv.ParseBool(raw)
		if err != nil {
			return v, err
		}
		v.SetBool(bv)
	default:
		return v, fmt.Errorf("不支持的参数类型 %s", t.Kind())
	}
	return v, nil
}

// writeJSON 写出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// writeError 写出错误响应。
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
