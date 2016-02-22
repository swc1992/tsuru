// Copyright 2016 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package redis

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/log"
	"gopkg.in/redis.v3"
)

var (
	ErrNoRedisConfig = errors.New("no redis configuration found with config prefix")
)

type Client interface {
	Exists(key string) *redis.BoolCmd
	RPush(key string, values ...string) *redis.IntCmd
	Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Get(key string) *redis.StringCmd
	Del(keys ...string) *redis.IntCmd
	Ping() *redis.StatusCmd
	LRange(key string, start, stop int64) *redis.StringSliceCmd
	LRem(key string, count int64, value interface{}) *redis.IntCmd
	Auth(password string) *redis.StatusCmd
	Select(index int64) *redis.StatusCmd
	Keys(pattern string) *redis.StringSliceCmd
	LLen(key string) *redis.IntCmd
}

type PubSubClient interface {
	Client
	Subscribe(channels ...string) (*redis.PubSub, error)
	Publish(channel, message string) *redis.IntCmd
}

type CommonConfig struct {
	DB           int64
	Password     string
	MaxRetries   int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
	PoolTimeout  time.Duration
	IdleTimeout  time.Duration
	TryLegacy    bool
	TryLocal     bool
}

func newRedisSentinel(addrs []string, master string, redisConfig *CommonConfig) (Client, error) {
	client := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    master,
		SentinelAddrs: addrs,
		DB:            redisConfig.DB,
		Password:      redisConfig.Password,
		MaxRetries:    redisConfig.MaxRetries,
		DialTimeout:   redisConfig.DialTimeout,
		ReadTimeout:   redisConfig.ReadTimeout,
		WriteTimeout:  redisConfig.WriteTimeout,
		PoolSize:      redisConfig.PoolSize,
		PoolTimeout:   redisConfig.PoolTimeout,
		IdleTimeout:   redisConfig.IdleTimeout,
	})
	err := client.Ping().Err()
	return client, err
}

func redisCluster(addrs []string, redisConfig *CommonConfig) (Client, error) {
	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:        addrs,
		Password:     redisConfig.Password,
		DialTimeout:  redisConfig.DialTimeout,
		ReadTimeout:  redisConfig.ReadTimeout,
		WriteTimeout: redisConfig.WriteTimeout,
		PoolSize:     redisConfig.PoolSize,
		PoolTimeout:  redisConfig.PoolTimeout,
		IdleTimeout:  redisConfig.IdleTimeout,
	})
	err := client.Ping().Err()
	return client, err
}

func redisServer(addr string, redisConfig *CommonConfig) (Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		DB:           redisConfig.DB,
		Password:     redisConfig.Password,
		MaxRetries:   redisConfig.MaxRetries,
		DialTimeout:  redisConfig.DialTimeout,
		ReadTimeout:  redisConfig.ReadTimeout,
		WriteTimeout: redisConfig.WriteTimeout,
		PoolSize:     redisConfig.PoolSize,
		PoolTimeout:  redisConfig.PoolTimeout,
		IdleTimeout:  redisConfig.IdleTimeout,
	})
	err := client.Ping().Err()
	return client, err
}

func NewRedis(prefix string) (Client, error) {
	return NewRedisDefaultConfig(prefix, &CommonConfig{
		PoolSize:    1000,
		PoolTimeout: time.Second,
		IdleTimeout: 2 * time.Minute,
	})
}

func NewRedisDefaultConfig(prefix string, defaultConfig *CommonConfig) (Client, error) {
	db, err := config.GetInt(prefix + ":redis-db")
	if err != nil && defaultConfig.TryLegacy {
		db, err = config.GetInt(prefix + ":db")
	}
	if err == nil {
		defaultConfig.DB = int64(db)
	}
	password, err := config.GetString(prefix + ":redis-password")
	if err != nil && defaultConfig.TryLegacy {
		password, err = config.GetString(prefix + ":password")
	}
	if err == nil {
		defaultConfig.Password = password
	}
	poolSize, err := config.GetInt(prefix + ":redis-pool-size")
	if err == nil {
		defaultConfig.PoolSize = poolSize
	}
	maxRetries, err := config.GetInt(prefix + ":redis-max-retries")
	if err == nil {
		defaultConfig.MaxRetries = maxRetries
	}
	poolTimeout, err := config.GetFloat(prefix + ":redis-pool-timeout")
	if err == nil {
		defaultConfig.PoolTimeout = time.Duration(poolTimeout * float64(time.Second))
	}
	idleTimeout, err := config.GetFloat(prefix + ":redis-pool-idle-timeout")
	if err == nil {
		defaultConfig.IdleTimeout = time.Duration(idleTimeout * float64(time.Second))
	}
	dialTimeout, err := config.GetFloat(prefix + ":redis-dial-timeout")
	if err == nil {
		defaultConfig.DialTimeout = time.Duration(dialTimeout * float64(time.Second))
	}
	readTimeout, err := config.GetFloat(prefix + ":redis-read-timeout")
	if err == nil {
		defaultConfig.ReadTimeout = time.Duration(readTimeout * float64(time.Second))
	}
	writeTimeout, err := config.GetFloat(prefix + ":redis-write-timeout")
	if err == nil {
		defaultConfig.WriteTimeout = time.Duration(writeTimeout * float64(time.Second))
	}
	sentinels, err := config.GetString(prefix + ":redis-sentinel-addrs")
	if err == nil {
		masterName, _ := config.GetString(prefix + ":redis-sentinel-master")
		if masterName == "" {
			return nil, fmt.Errorf("%s:redis-sentinel-master must be specified if using redis-sentinel", prefix)
		}
		log.Debugf("Connecting to redis sentinel from %q config prefix. Addrs: %s. Master: %s. DB: %d.", prefix, sentinels, masterName, db)
		return newRedisSentinel(strings.Split(sentinels, ","), masterName, defaultConfig)
	}
	cluster, err := config.GetString(prefix + ":redis-cluster-addrs")
	if err == nil {
		if defaultConfig.DB != 0 {
			return nil, fmt.Errorf("could not initialize redis from %q config, using redis-cluster with db != 0 is not supported", prefix)
		}
		if defaultConfig.MaxRetries != 0 {
			return nil, fmt.Errorf("could not initialize redis from %q config, using redis-cluster with max-retries > 0 is not supported", prefix)
		}
		log.Debugf("Connecting to redis cluster from %q config prefix. Addrs: %s. DB: %d.", prefix, cluster, db)
		return redisCluster(strings.Split(cluster, ","), defaultConfig)
	}
	server, err := config.GetString(prefix + ":redis-server")
	if err == nil {
		log.Debugf("Connecting to redis server from %q config prefix. Addr: %s. DB: %d.", prefix, server, db)
		return redisServer(server, defaultConfig)
	}
	host, err := config.GetString(prefix + ":redis-host")
	if err != nil && defaultConfig.TryLegacy {
		host, err = config.GetString(prefix + ":host")
	}
	if err == nil {
		portStr := "6379"
		port, err := config.Get(prefix + ":redis-port")
		if err != nil && defaultConfig.TryLegacy {
			port, err = config.Get(prefix + ":port")
		}
		if err == nil {
			portStr = fmt.Sprintf("%v", port)
		}
		addr := fmt.Sprintf("%s:%s", host, portStr)
		log.Debugf("Connecting to redis host/port from %q config prefix. Addr: %s. DB: %d.", prefix, addr, db)
		return redisServer(addr, defaultConfig)
	}
	if defaultConfig.TryLocal {
		addr := "localhost:6379"
		log.Debugf("Connecting to redis on localhost from %q config prefix. Addr: %s. DB: %d.", prefix, addr, db)
		return redisServer(addr, defaultConfig)
	}
	return nil, ErrNoRedisConfig
}
