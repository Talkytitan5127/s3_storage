# Задача 3: Завершение тестового покрытия и подготовка к production

## Статус выполнения Task 2

✅ **Завершено:**
- Consistent Hashing Algorithm (10 тестов, 95% покрытие)
- Chunking Logic (11 тестов, 90% покрытие)
- Database Operations PostgreSQL (15 тестов, готовы к запуску)
- SQL схема базы данных
- Инфраструктура проекта

**Прогресс:** 3/10 тестовых наборов (30%)

---

## Обзор выполненной работы Task 2

### 1. Database Operations (P0 - КРИТИЧНО) ✅

**Созданные файлы:**
- `migrations/001_initial_schema.sql` (115 строк) - Полная схема БД
- `internal/storage/postgres.go` (565 строк) - Реализация
- `internal/storage/postgres_test.go` (682 строки) - 15 комплексных тестов

**Реализованные тесты:**
1. ✅ TestCreateFile_Success - Создание файла с генерацией UUID
2. ✅ TestCreateFile_DuplicateID - Обработка дубликатов
3. ✅ TestCreateChunks_Batch - Batch insert 6 chunks
4. ✅ TestGetFile_ByID - Получение файла с JOIN chunks
5. ✅ TestGetFile_NotFound - Обработка ErrNotFound
6. ✅ TestUpdateFileStatus - Обновление статуса с updated_at
7. ✅ TestGetChunksByFileID - Получение chunks с сортировкой
8. ✅ TestTransaction_Rollback - Откат транзакции
9. ✅ TestTransaction_Commit - Commit транзакции
10. ✅ TestStorageServerRegistration - Регистрация сервера + 150 узлов
11. ✅ TestStorageServerHeartbeat - Обновление heartbeat
12. ✅ TestGetActiveStorageServers - Фильтрация по heartbeat
13. ✅ TestUploadSession_Create - Создание сессии с TTL
14. ✅ TestUploadSession_Cleanup - Cleanup expired sessions
15. ✅ TestConcurrentWrites - 10 goroutines без deadlocks

**Схема базы данных:**
- `storage_servers` - Реестр storage серверов
- `hash_ring_nodes` - 150 виртуальных узлов на сервер
- `files` - Метаданные файлов
- `chunks` - Информация о частях (6 на файл)
- `upload_sessions` - Отслеживание незавершенных загрузок

**Ключевые особенности:**
- UUID для всех primary keys
- Foreign keys с CASCADE
- Triggers для auto-update updated_at
- Indexes на критических полях
- CHECK constraints для валидации
- ACID транзакции

**Статус:** Тесты написаны и готовы к запуску. Требуется Docker для testcontainers.

---

## Цели Task 3

Завершить оставшиеся P0 и P1 тесты, настроить инфраструктуру и подготовить систему к production deployment.

---

## Раздел 1: P0 Integration тесты (КРИТИЧНО)

### 1.1 End-to-End Upload/Download Flow

**Ответственный:** QA + Backend Team  
**Срок:** 4 дня  
**Файл:** `tests/integration/e2e_test.go`  
**Приоритет:** 🔴 P0 - КРИТИЧНО

**Предварительные требования:**
- Docker Compose с PostgreSQL + API Gateway + 6 Storage Servers
- Реализация API Gateway (REST API)
- Реализация Storage Server (gRPC)
- Protobuf определения для gRPC

**Тесты для реализации (10 тестов):**

1. **TestUploadDownload_SmallFile**
   - Upload файла 10 MB через REST API
   - Проверка: 201 Created, file_id возвращен
   - Download файла по file_id
   - Проверка: содержимое идентично оригиналу (SHA-256)

2. **TestUploadDownload_LargeFile**
   - Upload файла 5 GB
   - Проверка: файл разделен на 6 chunks
   - Проверка: chunks распределены по разным storage серверам
   - Download и проверка целостности

3. **TestUploadDownload_MaxSize**
   - Upload файла 10 GB (максимум)
   - Проверка: успешная загрузка
   - Download и проверка

4. **TestUpload_ExceedsMaxSize**
   - Попытка upload файла 11 GB
   - Проверка: возвращается 413 Payload Too Large

5. **TestUpload_InvalidContentType**
   - Upload без Content-Type header
   - Проверка: возвращается 400 Bad Request

6. **TestDownload_NonExistentFile**
   - GET /files/{invalid-uuid}
   - Проверка: возвращается 404 Not Found

7. **TestListFiles**
   - Upload 10 файлов
   - GET /files (list endpoint)
   - Проверка: все 10 файлов в списке
   - Проверка: pagination работает

8. **TestDeleteFile**
   - Upload файла
   - DELETE /files/{file_id}
   - Проверка: файл удален из БД
   - Проверка: chunks удалены с storage серверов

9. **TestGetFileMetadata**
   - Upload файла
   - GET /files/{file_id}/metadata
   - Проверка: возвращаются все метаданные

10. **TestUploadProgress**
    - Upload большого файла
    - Периодически проверять статус через API
    - Проверка: статус меняется pending → uploading → completed

**Критерии приемки:**
- ✅ Все тесты проходят в Docker Compose окружении
- ✅ Тесты изолированы (cleanup после каждого)
- ✅ Время выполнения < 10 минут

---

## Раздел 2: Реализация компонентов для E2E тестов

### 2.1 Protobuf определения

**Файл:** `api/proto/storage.proto`  
**Приоритет:** 🔴 P0

```protobuf
syntax = "proto3";

package storage;
option go_package = "github.com/s3storage/api/proto";

service StorageService {
  rpc PutChunk(stream PutChunkRequest) returns (PutChunkResponse);
  rpc GetChunk(GetChunkRequest) returns (stream GetChunkResponse);
  rpc DeleteChunk(DeleteChunkRequest) returns (DeleteChunkResponse);
  rpc HealthCheck(HealthCheckRequest) returns (HealthCheckResponse);
}

message PutChunkRequest {
  string chunk_id = 1;
  bytes data = 2;
  string checksum = 3;
}

message PutChunkResponse {
  string chunk_id = 1;
  bool success = 2;
}

message GetChunkRequest {
  string chunk_id = 1;
}

message GetChunkResponse {
  bytes data = 1;
}

message DeleteChunkRequest {
  string chunk_id = 1;
}

message DeleteChunkResponse {
  bool success = 1;
}

message HealthCheckRequest {}

message HealthCheckResponse {
  string status = 1;
  int64 available_space = 2;
  int64 used_space = 3;
}
```

**Команды для генерации:**
```bash
protoc --go_out=. --go_opt=paths=source_relative \
       --go-grpc_out=. --go-grpc_opt=paths=source_relative \
       api/proto/storage.proto
```

### 2.2 API Gateway (минимальная реализация)

**Файл:** `cmd/api-gateway/main.go`  
**Приоритет:** 🔴 P0

**Endpoints:**
- POST /files - Upload файла
- GET /files/{file_id} - Download файла
- GET /files/{file_id}/metadata - Метаданные
- GET /files - List файлов
- DELETE /files/{file_id} - Удаление файла

**Ключевые компоненты:**
- Gin framework для REST API
- PostgreSQL connection pool
- gRPC clients для storage серверов
- Consistent hashing для распределения chunks
- Chunking logic для разделения файлов

### 2.3 Storage Server (минимальная реализация)

**Файл:** `cmd/storage-server/main.go`  
**Приоритет:** 🔴 P0

**Функциональность:**
- gRPC server реализация
- Сохранение chunks на диск (/data/chunks/{chunk_id})
- Heartbeat к PostgreSQL каждые 10 секунд
- Health check endpoint

---

## Раздел 3: P1 Unit тесты (Высокий приоритет)

### 3.1 gRPC Handlers

**Файл:** `internal/grpc/handlers_test.go`  
**Приоритет:** 🟡 P1  
**Срок:** 2 дня

**Тесты (10 тестов):**
1. TestPutChunk_Success - Mock gRPC stream, сохранение 1 GB chunk
2. TestPutChunk_InvalidChunkID - Невалидный chunk_id
3. TestPutChunk_DiskFull - Симуляция заполненного диска
4. TestGetChunk_Success - Streaming существующего chunk
5. TestGetChunk_NotFound - Несуществующий chunk_id
6. TestGetChunk_CorruptedFile - Checksum mismatch
7. TestDeleteChunk_Success - Удаление chunk с диска
8. TestHealthCheck - Проверка health endpoint
9. TestStreamingPerformance - Benchmark для 1 GB chunk
10. TestConcurrentStreams - 10 одновременных PutChunk

### 3.2 Interrupted Upload Handling

**Файл:** `tests/integration/interrupted_upload_test.go`  
**Приоритет:** 🟡 P1  
**Срок:** 2 дня

**Тесты (5 тестов):**
1. TestInterruptedUpload_ClientDisconnect
2. TestInterruptedUpload_ServerCrash
3. TestInterruptedUpload_NetworkTimeout
4. TestCleanupJob_ExpiredSessions
5. TestCleanupJob_ActiveSessions

### 3.3 Storage Server Management

**Файл:** `tests/integration/storage_management_test.go`  
**Приоритет:** 🟡 P1  
**Срок:** 2 дня

**Тесты (5 тестов):**
1. TestAddStorageServer_Dynamic
2. TestRemoveStorageServer
3. TestStorageServerFailover
4. TestHeartbeatMechanism
5. TestHashRingRefresh

### 3.4 Concurrent Operations

**Файл:** `tests/integration/concurrent_test.go`  
**Приоритет:** 🟡 P1  
**Срок:** 2 дня

**Тесты (5 тестов):**
1. TestConcurrentUploads - 50 goroutines
2. TestConcurrentDownloads - 100 goroutines
3. TestMixedOperations - uploads, downloads, deletes, list
4. TestDatabaseConnectionPool - 100 одновременных запросов
5. TestRaceConditions - Запуск с `-race` флагом

---

## Раздел 4: Docker Compose инфраструктура

### 4.1 Docker Compose для тестов

**Файл:** `docker-compose.test.yml`  
**Приоритет:** 🔴 P0  
**Срок:** 1 день

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: s3storage
      POSTGRES_USER: s3user
      POSTGRES_PASSWORD: s3pass
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./migrations:/docker-entrypoint-initdb.d
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U s3user"]
      interval: 5s
      timeout: 5s
      retries: 5

  api-gateway:
    build:
      context: .
      dockerfile: cmd/api-gateway/Dockerfile
    ports:
      - "8080:8080"
    environment:
      DATABASE_URL: postgres://s3user:s3pass@postgres:5432/s3storage
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 10s
      timeout: 5s
      retries: 3

  storage-1:
    build:
      context: .
      dockerfile: cmd/storage-server/Dockerfile
    environment:
      SERVER_ID: storage-1
      GRPC_PORT: 50051
      DATABASE_URL: postgres://s3user:s3pass@postgres:5432/s3storage
    volumes:
      - storage1_data:/data
    depends_on:
      postgres:
        condition: service_healthy

  storage-2:
    build:
      context: .
      dockerfile: cmd/storage-server/Dockerfile
    environment:
      SERVER_ID: storage-2
      GRPC_PORT: 50052
      DATABASE_URL: postgres://s3user:s3pass@postgres:5432/s3storage
    volumes:
      - storage2_data:/data
    depends_on:
      postgres:
        condition: service_healthy

  storage-3:
    build:
      context: .
      dockerfile: cmd/storage-server/Dockerfile
    environment:
      SERVER_ID: storage-3
      GRPC_PORT: 50053
      DATABASE_URL: postgres://s3user:s3pass@postgres:5432/s3storage
    volumes:
      - storage3_data:/data
    depends_on:
      postgres:
        condition: service_healthy

  storage-4:
    build:
      context: .
      dockerfile: cmd/storage-server/Dockerfile
    environment:
      SERVER_ID: storage-4
      GRPC_PORT: 50054
      DATABASE_URL: postgres://s3user:s3pass@postgres:5432/s3storage
    volumes:
      - storage4_data:/data
    depends_on:
      postgres:
        condition: service_healthy

  storage-5:
    build:
      context: .
      dockerfile: cmd/storage-server/Dockerfile
    environment:
      SERVER_ID: storage-5
      GRPC_PORT: 50055
      DATABASE_URL: postgres://s3user:s3pass@postgres:5432/s3storage
    volumes:
      - storage5_data:/data
    depends_on:
      postgres:
        condition: service_healthy

  storage-6:
    build:
      context: .
      dockerfile: cmd/storage-server/Dockerfile
    environment:
      SERVER_ID: storage-6
      GRPC_PORT: 50056
      DATABASE_URL: postgres://s3user:s3pass@postgres:5432/s3storage
    volumes:
      - storage6_data:/data
    depends_on:
      postgres:
        condition: service_healthy

volumes:
  postgres_data:
  storage1_data:
  storage2_data:
  storage3_data:
  storage4_data:
  storage5_data:
  storage6_data:

networks:
  default:
    name: s3storage_network
```

---

## Раздел 5: CI/CD Pipeline

### 5.1 GitHub Actions

**Файл:** `.github/workflows/test.yml`  
**Приоритет:** 🟡 P1  
**Срок:** 1 день

```yaml
name: Test Suite

on:
  push:
    branches: [ main, develop ]
  pull_request:
    branches: [ main, develop ]

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Install dependencies
        run: go mod download
      
      - name: Run unit tests
        run: go test -v -race -coverprofile=coverage.out ./internal/...
      
      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          file: ./coverage.out

  integration-tests:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:15-alpine
        env:
          POSTGRES_DB: s3storage_test
          POSTGRES_USER: testuser
          POSTGRES_PASSWORD: testpass
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
        ports:
          - 5432:5432
    
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Install dependencies
        run: go mod download
      
      - name: Run integration tests
        run: go test -v -race ./tests/integration/...
        env:
          DATABASE_URL: postgres://testuser:testpass@localhost:5432/s3storage_test

  e2e-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Start services
        run: docker-compose -f docker-compose.test.yml up -d
      
      - name: Wait for services
        run: sleep 30
      
      - name: Run E2E tests
        run: go test -v ./tests/integration/e2e_test.go
      
      - name: Stop services
        run: docker-compose -f docker-compose.test.yml down -v

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v3
        with:
          version: latest
```

---

## Раздел 6: Dockerfiles

### 6.1 API Gateway Dockerfile

**Файл:** `cmd/api-gateway/Dockerfile`

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o api-gateway ./cmd/api-gateway

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/api-gateway .
EXPOSE 8080

CMD ["./api-gateway"]
```

### 6.2 Storage Server Dockerfile

**Файл:** `cmd/storage-server/Dockerfile`

```dockerfile
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o storage-server ./cmd/storage-server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/storage-server .
RUN mkdir -p /data/chunks

EXPOSE 50051

CMD ["./storage-server"]
```

---

## План выполнения Task 3

### Неделя 1 (Дни 1-3): Protobuf и базовая реализация

- [ ] День 1: Создать protobuf определения, сгенерировать Go код
- [ ] День 2: Реализовать минимальный Storage Server с gRPC
- [ ] День 3: Реализовать минимальный API Gateway с REST API

### Неделя 1 (Дни 4-7): E2E тесты и Docker

- [ ] День 4: Создать Docker Compose конфигурацию
- [ ] День 5: Реализовать 5 основных E2E тестов
- [ ] День 6: Реализовать 5 дополнительных E2E тестов
- [ ] День 7: Отладка E2E тестов, проверка в Docker

### Неделя 2 (Дни 8-10): P1 gRPC тесты

- [ ] День 8: Реализовать gRPC handlers
- [ ] День 9: Реализовать 10 gRPC тестов
- [ ] День 10: Benchmarks и оптимизация

### Неделя 2 (Дни 11-14): P1 Integration тесты

- [ ] День 11: Interrupted Upload тесты (5 тестов)
- [ ] День 12: Storage Management тесты (5 тестов)
- [ ] День 13: Concurrent Operations тесты (5 тестов)
- [ ] День 14: CI/CD setup, финальная проверка

---

## Критерии завершения Task 3

### Обязательные (Must Have):
- ✅ Все P0 E2E тесты реализованы и проходят (10 тестов)
- ✅ Все P1 тесты реализованы и проходят (25 тестов)
- ✅ Docker Compose работает корректно
- ✅ CI/CD pipeline настроен и работает
- ✅ Code coverage ≥ 80% для всех компонентов
- ✅ Нет race conditions
- ✅ Документация обновлена

### Желательные (Should Have):
- ✅ Performance benchmarks для критических операций
- ✅ Load testing базовые сценарии
- ✅ Monitoring endpoints реализованы

### Опциональные (Nice to Have):
- ✅ Grafana dashboards для метрик
- ✅ Prometheus integration
- ✅ Distributed tracing (Jaeger)

---

## Метрики успеха

**После завершения Task 3:**
- Общий прогресс: 80-90% (8-9/10 тестовых наборов)
- Всего тестов: 71+ (21 из Task 1 + 15 из Task 2 + 35+ из Task 3)
- Code coverage: ≥ 80%
- Все P0 и P1 компоненты протестированы
- Система готова к production deployment
- CI/CD автоматизирован

---

## Технические детали

### Зависимости для добавления в go.mod:

```go
require (
    github.com/gin-gonic/gin v1.9.1
    google.golang.org/grpc v1.60.1
    google.golang.org/protobuf v1.32.0
)
```

### Структура проекта после Task 3:

```
s3_storage/
├── api/
│   └── proto/
│       ├── storage.proto
│       ├── storage.pb.go
│       └── storage_grpc.pb.go
├── cmd/
│   ├── api-gateway/
│   │   ├── main.go
│   │   └── Dockerfile
│   └── storage-server/
│       ├── main.go
│       └── Dockerfile
├── internal/
│   ├── hasher/          # ✅ Complete
│   ├── chunker/         # ✅ Complete
│   ├── storage/         # ✅ Complete
│   ├── grpc/            # 🚧 In Progress
│   │   ├── handlers.go
│   │   └── handlers_test.go
│   └── api/             # 🚧 In Progress
│       ├── routes.go
│       └── handlers.go
├── tests/
│   └── integration/
│       ├── e2e_test.go
│       ├── interrupted_upload_test.go
│       ├── storage_management_test.go
│       └── concurrent_test.go
├── migrations/          # ✅ Complete
├── docker-compose.test.yml
└── .github/
    └── workflows/
        └── test.yml
```

---

## Риски и митигация

### Риск 1: Сложность E2E тестов
**Митигация:** Начать с простых сценариев, постепенно усложнять

### Риск 2: Docker Compose производительность
**Митигация:** Использовать resource limits, оптимизировать образы

### Риск 3: Flaky тесты
**Митигация:** Добавить retry logic, увеличить timeouts, улучшить синхронизацию

### Риск 4: CI/CD время выполнения
**Митигация:** Параллелизация тестов, кеширование зависимостей

---

## Следующие шаги после Task 3

### Task 4: Production Readiness
- Мониторинг и алертинг (Prometheus + Grafana)
- Логирование (structured logging)
- Distributed tracing (Jaeger)
- Security hardening
- Performance optimization

### Task 5: Advanced Features
- Multipart upload API (S3-compatible)
- File versioning
- Access Control Lists (ACLs)
- Lifecycle policies
- CDN integration

---

**Создано:** 2026-01-06  
**Базируется на:** Task 1 (20% complete) + Task 2 (30% complete)  
**Ожидаемое время выполнения:** 2-3 недели  
**Приоритет:** Высокий (блокирует production release)  
**Статус:** Ready to start