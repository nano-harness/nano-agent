package agent

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/stretchr/testify/require"
)

type fakeBucket struct {
	deleted []string
	errs    map[string]error
}

func (b *fakeBucket) PutObject(objectKey string, reader io.Reader, options ...oss.Option) error {
	return nil
}

func (b *fakeBucket) GetObject(objectKey string, options ...oss.Option) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("{}")), nil
}

func (b *fakeBucket) IsObjectExist(objectKey string, options ...oss.Option) (bool, error) {
	return true, nil
}

func (b *fakeBucket) DeleteObject(objectKey string, options ...oss.Option) error {
	b.deleted = append(b.deleted, objectKey)
	if b.errs != nil {
		if err, ok := b.errs[objectKey]; ok {
			return err
		}
	}
	return nil
}

func TestOSSSessionStorage_DeleteSession_Cascades(t *testing.T) {
	b := &fakeBucket{}
	s := &OSSSessionStorage{bucket: b}

	err := s.DeleteSession("sess_1")
	require.NoError(t, err)
	require.Equal(t, []string{
		"sessions/sess_1.json",
		"sessions/sess_1/metadata.json",
		"sessions/sess_1/journal.jsonl",
	}, b.deleted)
}

func TestOSSSessionStorage_DeleteSession_IgnoresNoSuchKey(t *testing.T) {
	noSuchKey := oss.ServiceError{Code: "NoSuchKey"}
	b := &fakeBucket{
		errs: map[string]error{
			"sessions/sess_2.json":          noSuchKey,
			"sessions/sess_2/metadata.json": noSuchKey,
			"sessions/sess_2/journal.jsonl": noSuchKey,
		},
	}
	s := &OSSSessionStorage{bucket: b}

	err := s.DeleteSession("sess_2")
	require.NoError(t, err)
	require.Len(t, b.deleted, 3)
}

func TestOSSSessionStorage_DeleteSession_PropagatesOtherErrors(t *testing.T) {
	boom := errors.New("boom")
	b := &fakeBucket{
		errs: map[string]error{
			"sessions/sess_3.json": boom,
		},
	}
	s := &OSSSessionStorage{bucket: b}

	err := s.DeleteSession("sess_3")
	require.Error(t, err)
}
