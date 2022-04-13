package telephonist

import (
	"net/url"
	"testing"

	"github.com/go-playground/validator/v10"
)

var (
	dummyURL = &url.URL{
		Scheme: "https",
		Host:   "host",
	}
)

func TestURLIsRequired(t *testing.T) {
	_, err := NewClient(ClientOptions{APIKey: "123"})
	if err == nil {
		t.Fatal("Error expected, since URL is missing from ClientOptions, but got no error (nil)")
	}
}

func TestAPIKeyIsRequired(t *testing.T) {
	_, err := NewClient(ClientOptions{URL: dummyURL})
	if err == nil {
		t.Fatal("Error expected, since APIKey is missing from ClientOptions, but got no error (nil)")
	}
}

func TestValidatorOnHttpPost(t *testing.T) {
	client, err := NewClient(ClientOptions{
		APIKey: "123",
		URL:    dummyURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, cerr := client.httpPost("test", TaskTrigger{Name: "wrong-trigger", Body: nil}, nil)
	if cerr == nil {
		t.Fatal("invalid error: expected CombinedError with exactly 1 error, got: nil")
	}
	if len(cerr) != 1 {
		t.Fatalf("invalid error: expected CombinedError with exactly 1 error, got: %#v", cerr)
	}
	if _, ok := cerr[0].(validator.ValidationErrors); !ok {
		t.Fatalf("invalid error: expected CombinedError with exactly 1 error, got: %#v", cerr)
	}
}
