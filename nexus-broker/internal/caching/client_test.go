package caching

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

func TestCachingClient_CacheMissAndHit(t *testing.T) {
	// 1. Setup mock Redis server
	mr, err := miniredis.Run()
	assert.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// 2. Setup mock backend server
	handlerCallCount := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCallCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, world!"))
	}))
	defer mockServer.Close()

	// 3. Create caching client
	cachingClient := NewCachingClient(redisClient, 1*time.Minute)

	// 4. First request (cache miss)
	req, err := http.NewRequest("GET", mockServer.URL, nil)
	assert.NoError(t, err)

	resp1, err := cachingClient.Do(req)
	assert.NoError(t, err)
	defer resp1.Body.Close()

	body1, err := io.ReadAll(resp1.Body)
	assert.NoError(t, err)

	// 5. Assertions for cache miss
	assert.Equal(t, 1, handlerCallCount, "server should be hit once on cache miss")
	assert.Equal(t, http.StatusOK, resp1.StatusCode)
	assert.Equal(t, "Hello, world!", string(body1))

	// Check if the response was cached in Redis
	cached, err := mr.Get("http:" + mockServer.URL)
	assert.NoError(t, err)
	assert.NotEmpty(t, cached, "response should be cached in Redis")

	// 6. Second request (cache hit)
	req, err = http.NewRequest("GET", mockServer.URL, nil)
	assert.NoError(t, err)

	resp2, err := cachingClient.Do(req)
	assert.NoError(t, err)
	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	assert.NoError(t, err)

	// 7. Assertions for cache hit
	assert.Equal(t, 1, handlerCallCount, "server should not be hit again on cache hit")
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
	assert.Equal(t, string(body1), string(body2), "response from cache should be identical")
}

func TestCachingClient_PostRequest(t *testing.T) {
	// 1. Setup mock Redis server
	mr, err := miniredis.Run()
	assert.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// 2. Setup mock backend server
	handlerCallCount := 0
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCallCount++
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusCreated)
	}))
	defer mockServer.Close()

	// 3. Create caching client
	cachingClient := NewCachingClient(redisClient, 1*time.Minute)

	// 4. Send POST request
	req, err := http.NewRequest("POST", mockServer.URL, nil)
	assert.NoError(t, err)

	resp, err := cachingClient.Do(req)
	assert.NoError(t, err)
	defer resp.Body.Close()

	// 5. Assertions
	assert.Equal(t, 1, handlerCallCount, "server should be hit for POST request")
	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	// Check that nothing was cached
	keys := mr.Keys()
	assert.Empty(t, keys, "cache should be empty for non-GET request")
}