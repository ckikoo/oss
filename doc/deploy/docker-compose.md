# Docker Compose 部署（草稿）

说明：使用 Docker Compose 可以在开发或小规模测试环境快速启动服务及其依赖（MySQL、Redis）。以下示例假设将本仓库作为服务镜像构建。

示例 `docker-compose.yml`：

```yaml
version: '3.8'
services:
  oss:
    build: ..
    image: oss:local
    ports:
      - "8080:8080"
    environment:
      - MYSQL_HOST=mysql
      - MYSQL_PORT=3306
      - MYSQL_USER=root
      - MYSQL_PASSWORD=pass
      - REDIS_ADDR=redis:6379
    volumes:
      - ./data/storage:/var/lib/oss/storage
    depends_on:
      - mysql
      - redis

  mysql:
    image: mysql:8.0
    environment:
      MYSQL_ROOT_PASSWORD: pass
      MYSQL_DATABASE: oss
    volumes:
      - mysql-data:/var/lib/mysql

  redis:
    image: redis:7
    command: ["redis-server", "--appendonly", "yes"]
    volumes:
      - redis-data:/data

volumes:
  mysql-data:
  redis-data:
```

快速启动：

```bash
# 在 doc/deploy/ 或 repo 根目录运行
docker compose up --build -d
```

注意：
- 仅用于本地开发或集成测试；生产环境请参见“生产配置建议”。
- 请确保 `config.example.yaml` 中的占位配置与容器环境变量同步。
