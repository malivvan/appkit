package appkit

import (
	"errors"
	"strings"
	"testing"
)

func TestMakeFuncWrapperNotAFunction(t *testing.T) {
	_, err := wrapBinding("not a function")
	if err == nil {
		t.Fatal("expected error for non-function")
	}
}

func TestMakeFuncWrapperTooManyReturns(t *testing.T) {
	_, err := wrapBinding(func() (int, int, int) { return 0, 0, 0 })
	if err == nil {
		t.Fatal("expected error for too many return values")
	}
}

func TestMakeFuncWrapperSecondReturnNotError(t *testing.T) {
	_, err := wrapBinding(func() (int, int) { return 0, 0 })
	if err == nil {
		t.Fatal("expected error when second return is not error")
	}
}

func TestMakeFuncWrapperNoReturn(t *testing.T) {
	fn, err := wrapBinding(func(s string) {})
	if err != nil {
		t.Fatal(err)
	}
	val, err := fn("id", `["hello"]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil value, got %v", val)
	}
}

func TestMakeFuncWrapperReturnsValue(t *testing.T) {
	fn, err := wrapBinding(func(a, b int) int { return a + b })
	if err != nil {
		t.Fatal(err)
	}
	val, err := fn("id", `[3, 4]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 7 {
		t.Fatalf("expected 7, got %v", val)
	}
}

func TestMakeFuncWrapperReturnsError(t *testing.T) {
	fn, err := wrapBinding(func() error { return errors.New("boom") })
	if err != nil {
		t.Fatal(err)
	}
	_, err = fn("id", `[]`)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("expected 'boom' error, got %v", err)
	}
}

func TestMakeFuncWrapperReturnsNilError(t *testing.T) {
	fn, err := wrapBinding(func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	val, err := fn("id", `[]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil, got %v", val)
	}
}

func TestMakeFuncWrapperReturnsValueAndNilError(t *testing.T) {
	fn, err := wrapBinding(func(n int) (int, error) { return n * 2, nil })
	if err != nil {
		t.Fatal(err)
	}
	val, err := fn("id", `[21]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42 {
		t.Fatalf("expected 42, got %v", val)
	}
}

func TestMakeFuncWrapperReturnsValueAndError(t *testing.T) {
	fn, err := wrapBinding(func(n int) (int, error) {
		if n < 0 {
			return 0, errors.New("negative")
		}
		return n, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = fn("id", `[-1]`)
	if err == nil || err.Error() != "negative" {
		t.Fatalf("expected 'negative', got %v", err)
	}

	val, err := fn("id", `[5]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 5 {
		t.Fatalf("expected 5, got %v", val)
	}
}

func TestMakeFuncWrapperArgMismatch(t *testing.T) {
	fn, err := wrapBinding(func(a, b int) int { return a + b })
	if err != nil {
		t.Fatal(err)
	}
	_, err = fn("id", `[1]`)
	if err == nil {
		t.Fatal("expected error for argument mismatch")
	}
}

func TestMakeFuncWrapperBadJSON(t *testing.T) {
	fn, err := wrapBinding(func() {})
	if err != nil {
		t.Fatal(err)
	}
	_, err = fn("id", `not json`)
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestMakeFuncWrapperVariadic(t *testing.T) {
	fn, err := wrapBinding(func(nums ...int) int {
		sum := 0
		for _, n := range nums {
			sum += n
		}
		return sum
	})
	if err != nil {
		t.Fatal(err)
	}
	val, err := fn("id", `[1, 2, 3]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 6 {
		t.Fatalf("expected 6, got %v", val)
	}
}

func TestMakeFuncWrapperReturnsStruct(t *testing.T) {
	type result struct {
		Name string `json:"name"`
	}
	fn, err := wrapBinding(func(name string) (result, error) {
		return result{Name: name}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	val, err := fn("id", `["Alice"]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r, ok := val.(result)
	if !ok {
		t.Fatalf("expected result struct, got %T", val)
	}
	if r.Name != "Alice" {
		t.Fatalf("expected Alice, got %s", r.Name)
	}
}

func TestCallAndMarshalRecoversPanic(t *testing.T) {
	fn, err := wrapBinding(func() { panic("kaboom") })
	if err != nil {
		t.Fatal(err)
	}
	status, result := callBinding(fn, "id", `[]`)
	if status != -1 {
		t.Fatalf("status = %d, want -1", status)
	}
	if !strings.Contains(result, "panicked") || !strings.Contains(result, "kaboom") {
		t.Fatalf("result = %q, want it to mention the panic", result)
	}
}

func TestCallSchemeHandlerPassesThrough(t *testing.T) {
	h := func(req *contentRequest) *contentResponse {
		return &contentResponse{Body: []byte(req.URL), MIME: "text/plain"}
	}
	resp := invokeContentFunc(h, &contentRequest{URL: "app://x/y"})
	if resp == nil || string(resp.Body) != "app://x/y" {
		t.Fatalf("response = %+v, want body %q", resp, "app://x/y")
	}
}

func TestCallSchemeHandlerContainsPanic(t *testing.T) {
	h := func(*contentRequest) *contentResponse {
		panic("handler bug")
	}
	resp := invokeContentFunc(h, &contentRequest{URL: "app://x"})
	if resp != nil {
		t.Fatalf("response = %+v, want nil", resp)
	}
}
