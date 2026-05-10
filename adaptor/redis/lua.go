package redis

import "github.com/go-redis/redis/v8"

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

// luaBatchPop 原子批量弹出，避免多次 RPop 的竞态
// KEYS[1] = queue key, ARGV[1] = count
var luaBatchPop = redis.NewScript(`
	local result = {}
	local count = tonumber(ARGV[1])
	for i = 1, count do
		local val = redis.call('RPOP', KEYS[1])
		if not val then break end
		result[#result + 1] = val
	end
	return result
`)
