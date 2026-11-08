// cmd/redis-sentinel-check/main.go
//
// Утилита для chaos-теста Sentinel failover.
// Подключается к Redis через Sentinel (MasterName: "mymaster"),
// периодически делает PING и логирует, к какому master подключена.
// При failover (kill master → Sentinel повышает replica) —
// автоматически переподключается к новому master.
//
// Запуск:
//
//	go run ./cmd/redis-sentinel-check \
//	  -sentinel 172.28.0.12:26379 \
//	  -master mymaster \
//	  -interval 1s
//
// Утилита пишет в stdout строки формата:
//
//	[OK]   ping  master=172.28.0.10:6379  latency=1.2ms
//	[FAIL] ping  err=...                 (во время failover)
//
// sentinel-failover.sh парсит эти строки и проверяет, что после kill master
// клиент переключился на новый master.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	var (
		sentinelAddrs = flag.String("sentinel", "127.0.0.1:26379", "comma-separated Sentinel addresses")
		masterName    = flag.String("master", "mymaster", "Sentinel master name")
		interval      = flag.Duration("interval", 1*time.Second, "ping interval")
		password      = flag.String("password", "", "Redis password (optional)")
		timeout       = flag.Duration("timeout", 2*time.Second, "per-operation timeout")
	)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addrs := strings.Split(*sentinelAddrs, ",")
	for i := range addrs {
		addrs[i] = strings.TrimSpace(addrs[i])
	}

	// FailoverClient сам ходит через Sentinel, сам узнаёт текущий master
	// и сам переподключается при failover.
	rdb := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    *masterName,
		SentinelAddrs: addrs,
		Password:      *password,
		DialTimeout:   *timeout,
		ReadTimeout:   *timeout,
		WriteTimeout:  *timeout,
	})
	defer func() { _ = rdb.Close() }()

	logger := log.New(os.Stdout, "", log.LstdFlags)

	// Первый ping: ждём, пока Sentinel отдаст master.
	// Sentinel может стартовать чуть позже — ретраим до первого успеха.
	var lastMaster string
	for {
		start := time.Now()
		err := rdb.Ping(ctx).Err()
		if err == nil {
			lastMaster = masterAddr(ctx, rdb, addrs, *masterName)
			logger.Printf("[OK]   ping  master=%s  latency=%s", lastMaster, time.Since(start).Round(time.Microsecond))
			break
		}
		logger.Printf("[WAIT] ping  err=%v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(*interval):
		}
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Printf("[EXIT] context cancelled")
			return
		case <-ticker.C:
			start := time.Now()
			err := rdb.Ping(ctx).Err()
			if err != nil {
				logger.Printf("[FAIL] ping  err=%v", err)
				continue
			}
			cur := masterAddr(ctx, rdb, addrs, *masterName)
			lat := time.Since(start).Round(time.Microsecond)
			if cur != lastMaster {
				logger.Printf("[OK]   ping  master=%s  latency=%s  (FAILOVER: was %s)", cur, lat, lastMaster)
				lastMaster = cur
			} else {
				logger.Printf("[OK]   ping  master=%s  latency=%s", cur, lat)
			}
		}
	}
}

// masterAddr возвращает host:port текущего master'а, к которому подключён клиент.
// ClientInfo().Addr возвращает ЛОКАЛЬНЫЙ адрес клиента (например, 172.28.0.3:40034),
// а не адрес сервера. Поэтому спрашиваем у Sentinel: SENTINEL get-master-addr-by-name
// возвращает "<ip>\n<port>" текущего мастера (172.28.0.10:6379 или 172.28.0.11 после failover).
func masterAddr(ctx context.Context, rdb *redis.Client, sentinelAddrs []string, masterName string) string {
	// Идём в первый Sentinel (у нас один).
	sentinel := redis.NewSentinelClient(&redis.Options{Addr: sentinelAddrs[0]})
	defer func() { _ = sentinel.Close() }()

	addr, err := sentinel.GetMasterAddrByName(ctx, masterName).Result()
	if err != nil || len(addr) != 2 {
		// Fallback: если Sentinel недоступен, пробуем ClientInfo (локальный адрес).
		info, err2 := rdb.ClientInfo(ctx).Result()
		if err2 != nil || info == nil || info.Addr == "" {
			return "unknown"
		}
		return info.Addr
	}
	return fmt.Sprintf("%s:%s", addr[0], addr[1])
}
