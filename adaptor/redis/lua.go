package redis

import "github.com/go-redis/redis"

// 释放锁脚本：只有当锁的值等于指定的 uuid 时才删除
var luaUnlock = redis.NewScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`)

// 刷新锁脚本：只有当锁的值等于指定的 uuid 时才更新过期时间
var luaRefresh = redis.NewScript(`
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("pexpire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`)
