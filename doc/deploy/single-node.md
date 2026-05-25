# 单机部署指南（草稿）

说明：适用于在单台 Linux 服务器上直接运行二进制的部署方法，适合测试或小流量场景。

步骤：

1. 构建二进制：

```bash
make build
# 或者
GOOS=linux GOARCH=amd64 go build -o oss ./cmd/server
```

2. 准备目录和配置：

```bash
mkdir -p /opt/oss/{data,logs}
cp config.example.yaml /opt/oss/config.yaml
# 编辑 /opt/oss/config.yaml，设置 MySQL/Redis 等地址
```

3. 启动 MySQL/Redis（可用系统服务或容器）并初始化数据库：

```bash
# 用 docker 启动示例
docker run -d --name mysql -e MYSQL_ROOT_PASSWORD=pass -e MYSQL_DATABASE=oss mysql:8
docker run -d --name redis redis:7
# 在 mysql 中运行 init.sql 创建表
mysql -h127.0.0.1 -u root -ppass oss < init.sql
```

4. 启动服务：

```bash
/opt/oss/oss -c /opt/oss/config.yaml
```

5. 日志与监控：
- 日志写入 `/opt/oss/logs`（可在 `config.yaml` 配置）。
- 建议使用 systemd 管理服务，示例 unit 文件可加入后续文档。

注意：单机部署不具备高可用、自动扩缩容能力；生产使用需额外考虑 HA 与备份策略。