package rtconfig

import (
	"context"
	"fmt"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// Client owns a short-lived operator connection. It never changes service catalogs.
type Client struct {
	client *clientv3.Client
}

type Value struct {
	JSON     string
	Revision int64 // ModRevision of this key, zero if it does not exist.
	Exists   bool
}

func (c *Client) Close() error {
	return c.client.Close()
}

func (c *Client) Get(ctx context.Context, key string) (Value, error) {
	response, err := c.client.Get(ctx, key)
	if err != nil {
		return Value{}, fmt.Errorf("read runtime value: %w", err)
	}
	if len(response.Kvs) == 0 {
		return Value{}, nil
	}
	kv := response.Kvs[0]
	return Value{
		JSON:     string(kv.Value),
		Revision: kv.ModRevision,
		Exists:   true,
	}, nil
}

// Set compares the key revision before writing. An absent expected revision uses
// a fresh read, protecting against concurrent edits during this command. Passing
// an explicit revision also protects the operator's earlier read; zero means absent.
func (c *Client) Set(ctx context.Context, key, encoded string, expected *int64) (Value, error) {
	if expected != nil && *expected < 0 {
		return Value{}, fmt.Errorf("expected revision must not be negative")
	}
	var revision int64
	if expected == nil {
		previous, err := c.Get(ctx, key)
		if err != nil {
			return Value{}, err
		}
		revision = previous.Revision
	} else {
		revision = *expected
	}
	response, err := c.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(key), "=", revision)).
		Then(clientv3.OpPut(key, encoded)).
		Else(clientv3.OpGet(key)).
		Commit()
	if err != nil {
		// Do not report success or blindly repeat an operation whose response
		// may have been lost after commit. The caller can inspect it with get.
		return Value{}, fmt.Errorf("etcd did not confirm the write; check with hotelctl config get before retrying: %w", err)
	}
	if !response.Succeeded {
		var actual int64
		if len(response.Responses) != 0 {
			if current := response.Responses[0].GetResponseRange(); current != nil && len(current.Kvs) != 0 {
				actual = current.Kvs[0].ModRevision
			}
		}
		return Value{}, fmt.Errorf("runtime value changed: expected revision %d, actual %d; read it with hotelctl config get", revision, actual)
	}
	return Value{
		JSON:     encoded,
		Revision: response.Header.Revision,
		Exists:   true,
	}, nil
}
