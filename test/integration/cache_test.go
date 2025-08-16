package integration

import (
	"context"
	"crypto/rand"
	"io"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ruslanbaba/distributed-build-cache/pkg/grpc/server"
)

func TestCacheIntegration(t *testing.T) {
	// Connect to cache server
	conn, err := grpc.Dial("localhost:8080", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := server.NewBuildCacheServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("PutAndGet", func(t *testing.T) {
		testPutAndGet(t, client, ctx)
	})

	t.Run("Contains", func(t *testing.T) {
		testContains(t, client, ctx)
	})

	t.Run("LargeFile", func(t *testing.T) {
		testLargeFile(t, client, ctx)
	})
}

func testPutAndGet(t *testing.T, client server.BuildCacheServiceClient, ctx context.Context) {
	// Test data
	testData := []byte("Hello, Cache!")
	digest := &server.Digest{
		Hash:      "sha256:test123",
		SizeBytes: int64(len(testData)),
	}

	// Put request
	putStream, err := client.Put(ctx)
	if err != nil {
		t.Fatalf("Failed to create put stream: %v", err)
	}

	// Send metadata
	err = putStream.Send(&server.PutRequest{
		Metadata: &server.PutMetadata{
			Digest:       digest,
			InstanceName: "test",
			ContentType:  "application/octet-stream",
		},
	})
	if err != nil {
		t.Fatalf("Failed to send metadata: %v", err)
	}

	// Send data
	err = putStream.Send(&server.PutRequest{
		Data: testData,
	})
	if err != nil {
		t.Fatalf("Failed to send data: %v", err)
	}

	// Close and receive response
	putResp, err := putStream.CloseAndRecv()
	if err != nil {
		t.Fatalf("Failed to close put stream: %v", err)
	}

	if putResp.Digest.Hash != digest.Hash {
		t.Errorf("Expected hash %s, got %s", digest.Hash, putResp.Digest.Hash)
	}

	// Get request
	getStream, err := client.Get(ctx, &server.GetRequest{
		Digest:       digest,
		InstanceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to create get stream: %v", err)
	}

	// Read response
	var receivedData []byte
	for {
		resp, err := getStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to receive data: %v", err)
		}
		receivedData = append(receivedData, resp.Data...)
	}

	if string(receivedData) != string(testData) {
		t.Errorf("Expected data %s, got %s", string(testData), string(receivedData))
	}
}

func testContains(t *testing.T, client server.BuildCacheServiceClient, ctx context.Context) {
	digest := &server.Digest{
		Hash:      "sha256:test123",
		SizeBytes: 13,
	}

	resp, err := client.Contains(ctx, &server.ContainsRequest{
		Digests:      []*server.Digest{digest},
		InstanceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to check contains: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(resp.Results))
	}

	if !resp.Results[0].Exists {
		t.Error("Expected artifact to exist")
	}
}

func testLargeFile(t *testing.T, client server.BuildCacheServiceClient, ctx context.Context) {
	// Generate 10MB of random data
	dataSize := 10 * 1024 * 1024
	testData := make([]byte, dataSize)
	_, err := rand.Read(testData)
	if err != nil {
		t.Fatalf("Failed to generate test data: %v", err)
	}

	digest := &server.Digest{
		Hash:      "sha256:large_file_test",
		SizeBytes: int64(dataSize),
	}

	// Put large file
	putStream, err := client.Put(ctx)
	if err != nil {
		t.Fatalf("Failed to create put stream: %v", err)
	}

	// Send metadata
	err = putStream.Send(&server.PutRequest{
		Metadata: &server.PutMetadata{
			Digest:       digest,
			InstanceName: "test",
			ContentType:  "application/octet-stream",
		},
	})
	if err != nil {
		t.Fatalf("Failed to send metadata: %v", err)
	}

	// Send data in chunks
	chunkSize := 64 * 1024 // 64KB chunks
	for i := 0; i < len(testData); i += chunkSize {
		end := i + chunkSize
		if end > len(testData) {
			end = len(testData)
		}

		err = putStream.Send(&server.PutRequest{
			Data: testData[i:end],
		})
		if err != nil {
			t.Fatalf("Failed to send chunk: %v", err)
		}
	}

	// Close put stream
	_, err = putStream.CloseAndRecv()
	if err != nil {
		t.Fatalf("Failed to close put stream: %v", err)
	}

	// Get large file
	getStream, err := client.Get(ctx, &server.GetRequest{
		Digest:       digest,
		InstanceName: "test",
	})
	if err != nil {
		t.Fatalf("Failed to create get stream: %v", err)
	}

	// Read all data
	var receivedData []byte
	for {
		resp, err := getStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to receive data: %v", err)
		}
		receivedData = append(receivedData, resp.Data...)
	}

	if len(receivedData) != dataSize {
		t.Errorf("Expected %d bytes, got %d", dataSize, len(receivedData))
	}
}

func BenchmarkCachePut(b *testing.B) {
	conn, err := grpc.Dial("localhost:8080", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	client := server.NewBuildCacheServiceClient(conn)
	ctx := context.Background()

	testData := []byte(strings.Repeat("test data ", 1000))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			digest := &server.Digest{
				Hash:      fmt.Sprintf("sha256:bench_%d", i),
				SizeBytes: int64(len(testData)),
			}

			putStream, err := client.Put(ctx)
			if err != nil {
				b.Fatalf("Failed to create put stream: %v", err)
			}

			err = putStream.Send(&server.PutRequest{
				Metadata: &server.PutMetadata{
					Digest:       digest,
					InstanceName: "bench",
					ContentType:  "application/octet-stream",
				},
			})
			if err != nil {
				b.Fatalf("Failed to send metadata: %v", err)
			}

			err = putStream.Send(&server.PutRequest{
				Data: testData,
			})
			if err != nil {
				b.Fatalf("Failed to send data: %v", err)
			}

			_, err = putStream.CloseAndRecv()
			if err != nil {
				b.Fatalf("Failed to close put stream: %v", err)
			}

			i++
		}
	})
}
