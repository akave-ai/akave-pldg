package indexing

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"data-explorer/decoding"
	"data-explorer/utils"
)

func parseBenchBlockNums() ([]uint64, error) {
	raw := strings.TrimSpace(os.Getenv("AKAVE_BENCH_BLOCKS"))
	if raw == "" {
		// Small default range to keep benchmark runtime reasonable.
		// Override with AKAVE_BENCH_BLOCKS for your own dataset.
		raw = "1370696,1370697,1370700,1370701,1370704"
	}
	parts := strings.Split(raw, ",")
	out := make([]uint64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		u, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func fetchDecodeBlock(ctx context.Context, rpc *utils.RpcUrl, maxRetry int, blockNum uint64) (decoded int, failed int, err error) {
	// Fetch block metadata AND full tx list.
	b, rpcTxs, err := rpc.GetBlockWithTransactions(ctx, 1, maxRetry, blockNum)
	if err != nil {
		return 0, 0, err
	}
	// Use b.Num in case the RPC returns unexpected formatting.
	_ = b.Num

	for _, rpcTx := range rpcTxs {
		_, decErr := decoding.DecodeRPCTransaction(rpcTx)
		if decErr != nil {
			if isPermanentDecodeError(decErr) {
				// tx does not target our contract (or has no selector) => skip silently.
				continue
			}
			failed++
			continue
		}
		decoded++
	}
	return decoded, failed, nil
}

func benchmarkFetchDecodeBlocks(b *testing.B, concurrency int) {
	if os.Getenv("AKAVE_BENCH_REAL_RPC") != "1" {
		b.Skip("set AKAVE_BENCH_REAL_RPC=1 to run real RPC benchmarks")
	}
	if concurrency < 1 {
		concurrency = 1
	}

	rpcURL := os.Getenv("AKAVE_RPC_URL")
	if strings.TrimSpace(rpcURL) == "" {
		rpcURL = "https://c6-us.akave.ai/ext/bc/56g16Hr1SHQRzdM8JLm3GKYv7APVHY8T2TyeZLvDVzCaTRS7W/rpc"
	}

	maxRetry := 3
	if v := os.Getenv("AKAVE_BENCH_MAX_RETRY"); strings.TrimSpace(v) != "" {
		if u, err := strconv.Atoi(v); err == nil && u > 0 {
			maxRetry = u
		}
	}

	blockNums, err := parseBenchBlockNums()
	if err != nil {
		b.Fatalf("parse AKAVE_BENCH_BLOCKS: %v", err)
	}
	if len(blockNums) == 0 {
		b.Skip("no blocks configured")
	}

	// Warm up so the first network call doesn't dominate.
	{
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		rpc := utils.NewRpcUrl(rpcURL)
		for _, num := range blockNums {
			_, _, err := fetchDecodeBlock(ctx, rpc, maxRetry, num)
			if err != nil {
				cancel()
				b.Fatalf("warmup fetch/decode block %d: %v", num, err)
			}
		}
		cancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	rpc := utils.NewRpcUrl(rpcURL)

	b.ReportAllocs()
	b.ResetTimer()

	var decodedTotal, failedTotal int
	for i := 0; i < b.N; i++ {
		decodedTotal = 0
		failedTotal = 0

		if concurrency == 1 {
			for _, num := range blockNums {
				decoded, failed, err := fetchDecodeBlock(ctx, rpc, maxRetry, num)
				if err != nil {
					b.Fatalf("fetch/decode block %d: %v", num, err)
				}
				decodedTotal += decoded
				failedTotal += failed
			}
			continue
		}

		workers := concurrency
		numCh := make(chan uint64)
		var wg sync.WaitGroup
		var mu sync.Mutex
		errCh := make(chan error, 1)

		wg.Add(workers)
		for range workers {
			go func() {
				defer wg.Done()
				for num := range numCh {
					decoded, failed, err := fetchDecodeBlock(ctx, rpc, maxRetry, num)
					if err != nil {
						// Report back to the main goroutine; cancellation stops other workers.
						select {
						case errCh <- err:
						default:
						}
						cancel()
						return
					}
					mu.Lock()
					decodedTotal += decoded
					failedTotal += failed
					mu.Unlock()
				}
			}()
		}

		for _, num := range blockNums {
			select {
			case numCh <- num:
			case <-ctx.Done():
				b.Fatalf("benchmark context cancelled: %v", ctx.Err())
			}
		}
		close(numCh)
		wg.Wait()

		select {
		case workerErr := <-errCh:
			b.Fatalf("fetch/decode error: %v", workerErr)
		default:
		}
	}
		_ = decodedTotal
		_ = failedTotal
}

func BenchmarkFetchDecodeBlocks_Sequential(b *testing.B) {
	benchmarkFetchDecodeBlocks(b, 1)
}

func BenchmarkFetchDecodeBlocks_Parallel8(b *testing.B) {
	benchmarkFetchDecodeBlocks(b, 8)
}
