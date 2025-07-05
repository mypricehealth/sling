package sling

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	goquery "github.com/google/go-querystring/query"
)

const (
	contentType     = "Content-Type"
	jsonContentType = "application/json"
	formContentType = "application/x-www-form-urlencoded"
)

// Response is a clone of the http.Response but excluding the Body. Body
// is not included because it has already been consumed by Sling.
type Response struct {
	Status     string // e.g. "200 OK"
	StatusCode int    // e.g. 200
	Proto      string // e.g. "HTTP/1.0"
	ProtoMajor int    // e.g. 1
	ProtoMinor int    // e.g. 0

	// Header maps header keys to values. If the response had multiple
	// headers with the same key, they may be concatenated, with comma
	// delimiters.  (RFC 7230, section 3.2.2 requires that multiple headers
	// be semantically equivalent to a comma-delimited sequence.) When
	// Header values are duplicated by other fields in this struct (e.g.,
	// ContentLength, TransferEncoding, Trailer), the field values are
	// authoritative.
	//
	// Keys in the map are canonicalized (see CanonicalHeaderKey).
	Header http.Header

	// ContentLength records the length of the associated content. The
	// value -1 indicates that the length is unknown. Unless Request.Method
	// is "HEAD", values >= 0 indicate that the given number of bytes may
	// be read from Body.
	ContentLength int64

	// Contains transfer encodings from outer-most to inner-most. Value is
	// nil, means that "identity" encoding is used.
	TransferEncoding []string

	// Close records whether the header directed that the connection be
	// closed after reading Body. The value is advice for clients: neither
	// ReadResponse nor Response.Write ever closes a connection.
	Close bool

	// Uncompressed reports whether the response was sent compressed but
	// was decompressed by the http package. When true, reading from
	// Body yields the uncompressed content instead of the compressed
	// content actually set from the server, ContentLength is set to -1,
	// and the "Content-Length" and "Content-Encoding" fields are deleted
	// from the responseHeader. To get the original response from
	// the server, set Transport.DisableCompression to true.
	Uncompressed bool

	// Trailer maps trailer keys to values in the same
	// format as Header.
	//
	// The Trailer initially contains only nil values, one for
	// each key specified in the server's "Trailer" header
	// value. Those values are not added to Header.
	//
	// Trailer must not be accessed concurrently with Read calls
	// on the Body.
	//
	// After Body.Read has returned io.EOF, Trailer will contain
	// any trailer values sent by the server.
	Trailer http.Header

	// Request is the request that was sent to obtain this Response.
	// Request's Body is nil (having already been consumed).
	// This is only populated for Client requests.
	Request *http.Request

	// TLS contains information about the TLS connection on which the
	// response was received. It is nil for unencrypted responses.
	// The pointer is shared between responses and should not be
	// modified.
	TLS *tls.ConnectionState
}

func newResponse(resp *http.Response) *Response {
	return &Response{
		Status:           resp.Status,
		StatusCode:       resp.StatusCode,
		Proto:            resp.Proto,
		ProtoMajor:       resp.ProtoMajor,
		ProtoMinor:       resp.ProtoMinor,
		Header:           resp.Header,
		ContentLength:    resp.ContentLength,
		TransferEncoding: resp.TransferEncoding,
		Close:            resp.Close,
		Uncompressed:     resp.Uncompressed,
		Trailer:          resp.Trailer,
		Request:          resp.Request,
		TLS:              resp.TLS,
	}
}

// Doer executes http requests.  It is implemented by *http.Client.  You can
// wrap *http.Client with layers of Doers to form a stack of client-side
// middleware.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Sling is an HTTP Request builder and sender.
type Sling struct {
	// http Client for doing requests
	httpClient Doer
	// HTTP method (GET, POST, etc.)
	method string
	// raw url string for requests
	rawURL string
	// stores key-values pairs to add to request's Headers
	header http.Header
	// url tagged query structs
	queryStructs []interface{}
	// body provider
	bodyProvider BodyProvider
	// response decoder
	responseDecoder ResponseDecoder
}

// New returns a new Sling with an http DefaultClient.
func New() *Sling {
	return &Sling{
		httpClient:      http.DefaultClient,
		method:          "GET",
		header:          make(http.Header),
		queryStructs:    make([]interface{}, 0),
		responseDecoder: jsonDecoder{},
	}
}

// New returns a copy of a Sling for creating a new Sling with properties
// from a parent Sling. For example,
//
//	parentSling := sling.New().Client(client).Base("https://api.io/")
//	fooSling := parentSling.New().Get("foo/")
//	barSling := parentSling.New().Get("bar/")
//
// fooSling and barSling will both use the same client, but send requests to
// https://api.io/foo/ and https://api.io/bar/ respectively.
//
// Note that query and body values are copied so if pointer values are used,
// mutating the original value will mutate the value within the child Sling.
func (s *Sling) New() *Sling {
	// copy Headers pairs into new Header map
	headerCopy := make(http.Header)
	for k, v := range s.header {
		headerCopy[k] = v
	}
	return &Sling{
		httpClient:      s.httpClient,
		method:          s.method,
		rawURL:          s.rawURL,
		header:          headerCopy,
		queryStructs:    append([]interface{}{}, s.queryStructs...),
		bodyProvider:    s.bodyProvider,
		responseDecoder: s.responseDecoder,
	}
}

// Http Client

// Client sets the http Client used to do requests. If a nil client is given,
// the http.DefaultClient will be used.
func (s *Sling) Client(httpClient *http.Client) *Sling {
	if httpClient == nil {
		return s.Doer(http.DefaultClient)
	}
	return s.Doer(httpClient)
}

// Doer sets the custom Doer implementation used to do requests.
// If a nil client is given, the http.DefaultClient will be used.
func (s *Sling) Doer(doer Doer) *Sling {
	if doer == nil {
		s.httpClient = http.DefaultClient
	} else {
		s.httpClient = doer
	}
	return s
}

// Method

// Head sets the Sling method to HEAD and sets the given pathURL.
func (s *Sling) Head(pathURL string) *Sling {
	s.method = "HEAD"
	return s.Path(pathURL)
}

// Get sets the Sling method to GET and sets the given pathURL.
func (s *Sling) Get(pathURL string) *Sling {
	s.method = "GET"
	return s.Path(pathURL)
}

// Post sets the Sling method to POST and sets the given pathURL.
func (s *Sling) Post(pathURL string) *Sling {
	s.method = "POST"
	return s.Path(pathURL)
}

// Put sets the Sling method to PUT and sets the given pathURL.
func (s *Sling) Put(pathURL string) *Sling {
	s.method = "PUT"
	return s.Path(pathURL)
}

// Patch sets the Sling method to PATCH and sets the given pathURL.
func (s *Sling) Patch(pathURL string) *Sling {
	s.method = "PATCH"
	return s.Path(pathURL)
}

// Delete sets the Sling method to DELETE and sets the given pathURL.
func (s *Sling) Delete(pathURL string) *Sling {
	s.method = "DELETE"
	return s.Path(pathURL)
}

// Options sets the Sling method to OPTIONS and sets the given pathURL.
func (s *Sling) Options(pathURL string) *Sling {
	s.method = "OPTIONS"
	return s.Path(pathURL)
}

// Trace sets the Sling method to TRACE and sets the given pathURL.
func (s *Sling) Trace(pathURL string) *Sling {
	s.method = "TRACE"
	return s.Path(pathURL)
}

// Connect sets the Sling method to CONNECT and sets the given pathURL.
func (s *Sling) Connect(pathURL string) *Sling {
	s.method = "CONNECT"
	return s.Path(pathURL)
}

// Header

// Add adds the key, value pair in Headers, appending values for existing keys
// to the key's values. Header keys are canonicalized.
func (s *Sling) Add(key, value string) *Sling {
	s.header.Add(key, value)
	return s
}

// Set sets the key, value pair in Headers, replacing existing values
// associated with key. Header keys are canonicalized.
func (s *Sling) Set(key, value string) *Sling {
	s.header.Set(key, value)
	return s
}

// AddHeaders adds all the http.Header values, appending values for existing keys
// to the key's values. Header keys are canonicalized.
func (s *Sling) AddHeaders(headers http.Header) *Sling {
	for key, values := range headers {
		for i := range values {
			s.header.Add(key, values[i])
		}
	}
	return s
}

// SetHeaders sets all the http.Header values, replacing values for existing keys
// to the key's values. Header keys are canonicalized.
func (s *Sling) SetHeaders(headers http.Header) *Sling {
	for key, values := range headers {
		for i := range values {
			if i == 0 {
				s.header.Set(key, values[i])
			} else {
				s.header.Add(key, values[i])
			}
		}
	}
	return s
}

// SetBasicAuth sets the Authorization header to use HTTP Basic Authentication
// with the provided username and password. With HTTP Basic Authentication
// the provided username and password are not encrypted.
func (s *Sling) SetBasicAuth(username, password string) *Sling {
	return s.Set("Authorization", "Basic "+basicAuth(username, password))
}

// basicAuth returns the base64 encoded username:password for basic auth copied
// from net/http.
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}

// Url

// Base sets the rawURL. If you intend to extend the url with Path,
// baseUrl should be specified with a trailing slash.
func (s *Sling) Base(rawURL string) *Sling {
	s.rawURL = rawURL
	return s
}

// Path extends the rawURL with the given path by resolving the reference to
// an absolute URL. If parsing errors occur, the rawURL is left unmodified.
func (s *Sling) Path(path string) *Sling {
	baseURL, baseErr := url.Parse(s.rawURL)
	pathURL, pathErr := url.Parse(path)
	if baseErr == nil && pathErr == nil {
		s.rawURL = baseURL.ResolveReference(pathURL).String()
		return s
	}
	return s
}

// Method sets the http method directly. Convenience methods are also provided
// for all standard http methods
func (s *Sling) Method(method string) *Sling {
	s.method = method
	return s
}

// QueryStruct appends the queryStruct to the Sling's queryStructs. The value
// pointed to by each queryStruct will be encoded as url query parameters on
// new requests (see Request()).
// The queryStruct argument should be a pointer to a url tagged struct. See
// https://godoc.org/github.com/google/go-querystring/query for details.
func (s *Sling) QueryStruct(queryStruct interface{}) *Sling {
	if queryStruct != nil {
		s.queryStructs = append(s.queryStructs, queryStruct)
	}
	return s
}

// QueryValues appends url.Values to the query string. The value will be encoded as
// url query parameters on new requests (see Request()).
func (s *Sling) QueryValues(values url.Values) *Sling {
	if values != nil {
		s.queryStructs = append(s.queryStructs, values)
	}
	return s
}

// Body

// Body sets the Sling's body. The body value will be set as the Body on new
// requests (see Request()).
// If the provided body is also an io.Closer, the request Body will be closed
// by http.Client methods.
func (s *Sling) Body(body io.Reader) *Sling {
	if body == nil {
		return s
	}
	return s.BodyProvider(bodyProvider{body: body})
}

// BodyProvider sets the Sling's body provider.
func (s *Sling) BodyProvider(body BodyProvider) *Sling {
	if body == nil {
		return s
	}
	s.bodyProvider = body

	ct := body.ContentType()
	if ct != "" {
		s.Set(contentType, ct)
	}

	return s
}

// BodyJSON sets the Sling's bodyJSON. The value pointed to by the bodyJSON
// will be JSON encoded as the Body on new requests (see Request()).
// The bodyJSON argument should be a pointer to a JSON tagged struct. See
// https://golang.org/pkg/encoding/json/#MarshalIndent for details.
func (s *Sling) BodyJSON(bodyJSON interface{}) *Sling {
	if bodyJSON == nil {
		return s
	}
	return s.BodyProvider(jsonBodyProvider{payload: bodyJSON})
}

// BodyForm sets the Sling's bodyForm. The value pointed to by the bodyForm
// will be url encoded as the Body on new requests (see Request()).
// The bodyForm argument should be a pointer to a url tagged struct or a url.Values.
// See https://godoc.org/github.com/google/go-querystring/query for details.
func (s *Sling) BodyForm(bodyForm interface{}) *Sling {
	if bodyForm == nil {
		return s
	}
	return s.BodyProvider(formBodyProvider{payload: bodyForm})
}

// Requests

// Request returns a new http.Request created with the Sling properties.
// Returns any errors parsing the rawURL, encoding query structs, encoding
// the body, or creating the http.Request.
func (s *Sling) request() (*http.Request, error) {
	return s.requestWithContext(context.Background())
}

func (s *Sling) requestWithContext(ctx context.Context) (*http.Request, error) {
	reqURL, err := url.Parse(s.rawURL)
	if err != nil {
		return nil, err
	}

	err = addQueryStructs(reqURL, s.queryStructs)
	if err != nil {
		return nil, err
	}

	var body io.Reader
	if s.bodyProvider != nil {
		body, err = s.bodyProvider.Body()
		if err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, s.method, reqURL.String(), body)
	if err != nil {
		return nil, err
	}
	addHeaders(req, s.header)
	return req, err
}

// addQueryStructs parses url tagged query structs using go-querystring to
// encode them to url.Values and format them onto the url.RawQuery. Any
// query parsing or encoding errors are returned.
func addQueryStructs(reqURL *url.URL, queryStructs []interface{}) error {
	urlValues, err := url.ParseQuery(reqURL.RawQuery)
	if err != nil {
		return err
	}
	// encodes query structs into a url.Values map and merges maps
	for _, queryStruct := range queryStructs {
		queryValues, ok := queryStruct.(url.Values) // use url.Values directly if we have it
		if !ok {
			var err error
			queryValues, err = goquery.Values(queryStruct)
			if err != nil {
				return err
			}
		}
		for key, values := range queryValues {
			for _, value := range values {
				urlValues.Add(key, value)
			}
		}
	}
	// url.Values format to a sorted "url encoded" string, e.g. "key=val&foo=bar"
	reqURL.RawQuery = urlValues.Encode()
	return nil
}

// addHeaders adds the key, value pairs from the given http.Header to the
// request. Values for existing keys are appended to the keys values.
func addHeaders(req *http.Request, header http.Header) {
	for key, values := range header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
}

// Sending

// ResponseDecoder sets the Sling's response decoder.
func (s *Sling) ResponseDecoder(decoder ResponseDecoder) *Sling {
	if decoder == nil {
		return s
	}
	s.responseDecoder = decoder
	return s
}

// ReceiveSuccess creates a new HTTP request and returns the response. Success
// responses (2XX) are JSON decoded into the value pointed to by successV.
// Any error creating the request, sending it, or decoding a 2XX response
// is returned.
func (s *Sling) ReceiveSuccess(successV interface{}) (*Response, error) {
	return s.Receive(successV, nil)
}

// Receive creates a new HTTP request and returns the response. Success
// responses (2XX) are JSON decoded into the value pointed to by successV and
// other responses are JSON decoded into the value pointed to by failureV.
// If the status code of response is 204(no content) or the Content-Lenght is 0,
// decoding is skipped. Any error creating the request, sending it, or decoding
// the response is returned.
// Receive is shorthand for calling Request and Do.
func (s *Sling) Receive(successV, failureV interface{}) (*Response, error) {
	return s.ReceiveWithContext(context.Background(), successV, failureV)
}

// ReceiveWithContext creates a new HTTP request and returns the response. Success
// responses (2XX) are JSON decoded into the value pointed to by successV and
// other responses are JSON decoded into the value pointed to by failureV.
// If the status code of response is 204(no content) or the Content-Lenght is 0,
// decoding is skipped. Any error creating the request, sending it, or decoding
// the response is returned.
// Receive is shorthand for calling Request and Do.
func (s *Sling) ReceiveWithContext(ctx context.Context, successV, failureV interface{}) (*Response, error) {
	req, err := s.requestWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return s.doDecode(req, successV, failureV)
}

func (s *Sling) Do(ctx context.Context) (*http.Response, error) {
	req, err := s.requestWithContext(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := s.do(req)
	if err != nil {
		return resp, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, fmt.Errorf("status code %d was not successful", resp.StatusCode)
	}

	return resp, nil
}

func (s *Sling) do(req *http.Request) (*http.Response, error) {
	resp, err := s.httpClient.Do(req)
	if err != nil {
		if err == context.Canceled {
			ctxErr := context.Cause(req.Context())
			if ctxErr != nil {
				err = ctxErr
			}
		}
	}

	return resp, err
}

// Do sends an HTTP request and returns the response. Success responses (2XX)
// are JSON decoded into the value pointed to by successV and other responses
// are JSON decoded into the value pointed to by failureV.
// If the status code of response is 204(no content) or the Content-Length is 0,
// decoding is skipped. Any error sending the request or decoding the response
// is returned.
func (s *Sling) doDecode(req *http.Request, successV, failureV interface{}) (*Response, error) {
	resp, err := s.do(req)
	if err != nil {
		return newResponse(resp), err
	}
	// when err is nil, resp contains a non-nil resp.Body which must be closed
	defer resp.Body.Close()

	// The default HTTP client's Transport may not
	// reuse HTTP/1.x "keep-alive" TCP connections if the Body is
	// not read to completion and closed.
	// See: https://golang.org/pkg/net/http/#Response
	defer io.Copy(io.Discard, resp.Body)

	// Don't try to decode on 204s
	if resp.StatusCode == http.StatusNoContent {
		return newResponse(resp), nil
	}

	// Don't decode if the content length is 0
	if resp.ContentLength == 0 {
		if failureV == nil && !isSuccessful(resp.StatusCode) {
			return newResponse(resp), fmt.Errorf("status code %d was not successful and had no body", resp.StatusCode)
		}

		return newResponse(resp), nil
	}

	// Decode the body
	if successV != nil || failureV != nil {
		err = decodeResponse(resp, s.responseDecoder, successV, failureV)
	}
	return newResponse(resp), err
}

// decodeResponse decodes response Body into the value pointed to by successV
// if the response is a success (2XX) or into the value pointed to by failureV
// otherwise. If the successV or failureV argument to decode into is nil,
// decoding is skipped.
// Caller is responsible for closing the resp.Body.
func decodeResponse(resp *http.Response, decoder ResponseDecoder, successV, failureV interface{}) error {
	if code := resp.StatusCode; isSuccessful(code) {
		if successV != nil {
			return decoder.Decode(resp, successV)
		}
	} else {
		if failureV != nil {
			return decoder.Decode(resp, failureV)
		}

		body, err := readWithCap(resp.Body, 100)
		if err != nil {
			return fmt.Errorf("status code %d was not successful and could not get body: %w", code, err)
		}

		return fmt.Errorf("status code %d was not successful, got body: %s", code, body)
	}
	return nil
}

func isSuccessful(code int) bool {
	return 200 <= code && code <= 299
}

func readWithCap(r io.Reader, cap int) (string, error) {
	maxBodySize := 100

	// Read one past the maximum body size to distinguish between a body with a length that's exactly `maxBodySize` and a body that's larger
	buf := make([]byte, maxBodySize+1)
	n, err := io.ReadFull(r, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}

	if n == 0 {
		return "", fmt.Errorf("got no response content")
	}

	var body string
	if n <= maxBodySize {
		body = string(buf[:n])
	} else {
		// If the body is larger than the maxBodySize, it has to be truncated
		body = string(buf[:maxBodySize]) + " (truncated)"
	}

	return body, nil
}
