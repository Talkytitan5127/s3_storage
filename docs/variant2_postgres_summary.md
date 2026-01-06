# Вариант 2 с PostgreSQL: Резюме и ключевые решения

## Выбор архитектуры

Выбран **Вариант 2: Распределенная архитектура с Consistent Hashing**, адаптированный для использования **PostgreSQL вместо Redis**.

## Почему PostgreSQL вместо Redis?

### Ключевые преимущества

#### 1. Надежность и Durability
- **ACID транзакции** обеспечивают строгую консистентность метаданных
- **Write-Ahead Log (WAL)** гарантирует, что данные не потеряются при сбоях
- Redis с AOF/RDB имеет риск потери данных при крашах
- Соответствует требованию **strong consistency** из задания

#### 2. Структурированные данные
- Реляционная модель идеально подходит для метаданных файлов и chunks
- Foreign keys обеспечивают referential integrity
- Сложные JOIN запросы для аналитики и отчетов
- JSONB для гибкого хранения дополнительных метаданных

#### 3. Операционные преимущества
- Зрелая экосистема backup/restore инструментов
- Богатый выбор мониторинга (pgAdmin, Grafana, Prometheus)
- Простая отладка через SQL запросы
- Управление схемой через миграции

#### 4. Масштабируемость
- Streaming replication для read replicas
- Партиционирование таблиц при росте данных
- Connection pooling (pgBouncer)
- Вертикальное масштабирование + горизонтальное чтение

### Компенсация недостатков Redis

**Проблема:** Redis быстрее для простых операций  
**Решение:** In-memory кеш hash ring в API Gateway минимизирует обращения к БД

**Проблема:** Redis лучше для высоконагруженных систем  
**Решение:** 
- Prepared statements и connection pooling
- Индексы на всех критических полях
- Batch операции где возможно
- Для MVP производительности PostgreSQL достаточно

## Ключевые архитектурные решения

### 1. Consistent Hashing

**Зачем:** Детерминированное распределение chunks с минимальным перераспределением при добавлении серверов

**Как работает:**
- Каждый storage сервер представлен 150 виртуальными узлами на hash ring
- Hash функция: xxHash (быстрая и качественная)
- Chunk размещается на первом сервере по часовой стрелке от его hash значения
- При добавлении нового сервера перераспределяется только ~1/N chunks

**Преимущества:**
- Равномерное распределение нагрузки
- Легкое добавление новых storage серверов
- Детерминированность (один chunk всегда на одном сервере)
- O(log N) поиск сервера

### 2. gRPC с Streaming

**Зачем:** Эффективная передача больших файлов (до 10 GiB)

**Преимущества:**
- Бинарный протокол (меньше overhead чем REST)
- Bidirectional streaming для upload/download
- HTTP/2 multiplexing
- Type-safe контракты через protobuf
- Встроенная поддержка в Go

### 3. Chunking Strategy

**Параметры:**
- Количество chunks: **6** (фиксировано по требованиям)
- Размер chunk: `file_size / 6` (равные части)
- Для файла 10 GiB: ~1.67 GiB на chunk

**Процесс:**
1. API Gateway получает файл
2. Разделяет на 6 равных частей в памяти (streaming)
3. Для каждого chunk вычисляет storage сервер через consistent hashing
4. Параллельно отправляет chunks на storage серверы через gRPC
5. Записывает метаданные в PostgreSQL

### 4. Обработка прерванных загрузок

**Проблема:** Пользователь может уйти во время загрузки

**Решение:**
- Таблица `upload_sessions` с полем `expires_at`
- При старте загрузки создается сессия с TTL 1 час
- Background job каждые 5 минут проверяет истекшие сессии
- Удаляет неполные chunks с storage серверов
- Очищает метаданные из БД

**Альтернатива:** Multipart upload API (как в S3) для resume capability

### 5. Storage Server Registration

**Процесс:**
1. Storage сервер стартует
2. Регистрируется в PostgreSQL (`storage_servers` таблица)
3. Создает 150 виртуальных узлов в `hash_ring_nodes`
4. Отправляет heartbeat каждые 10 секунд
5. API Gateway периодически обновляет in-memory hash ring

**Преимущества:**
- Динамическое добавление серверов без перезапуска API Gateway
- Автоматическое обнаружение недоступных серверов
- Централизованное управление топологией

## Схема базы данных

### Основные таблицы

**files** - метаданные файлов
- file_id (UUID, PK)
- filename, content_type, total_size
- upload_status (pending, uploading, completed, failed)
- checksum (SHA-256)

**chunks** - информация о частях файлов
- chunk_id (UUID, PK)
- file_id (FK → files)
- chunk_number (0-5)
- storage_server_id (FK → storage_servers)
- chunk_hash (SHA-256)

**storage_servers** - реестр storage серверов
- server_id (UUID, PK)
- grpc_address, status
- available_space, used_space
- last_heartbeat

**hash_ring_nodes** - виртуальные узлы для consistent hashing
- node_id (UUID, PK)
- server_id (FK → storage_servers)
- virtual_node_index (0-149)
- hash_value (BIGINT)

**upload_sessions** - отслеживание незавершенных загрузок
- session_id (UUID, PK)
- file_id (FK → files)
- expires_at, status

## Workflow примеры

### Upload файла 6 GiB

```
1. Client → API Gateway: POST /files (multipart/form-data)
2. API Gateway: Generate file_id, create upload_session
3. API Gateway: Split file into 6 chunks (~1 GiB each)
4. API Gateway → PostgreSQL: INSERT files, chunks, upload_session
5. For each chunk (parallel):
   - Calculate storage server via consistent hashing
   - API Gateway → Storage Server: gRPC PutChunk(stream)
   - Storage Server: Save to disk /data/chunks/{chunk_id}
   - API Gateway → PostgreSQL: UPDATE chunk status='completed'
6. API Gateway → PostgreSQL: UPDATE file status='completed'
7. API Gateway → Client: 201 Created {file_id}
```

### Download файла

```
1. Client → API Gateway: GET /files/{file_id}
2. API Gateway → PostgreSQL: SELECT file metadata
3. API Gateway → PostgreSQL: SELECT chunks ORDER BY chunk_number
4. API Gateway → Client: Start streaming response
5. For each chunk (sequential):
   - API Gateway → Storage Server: gRPC GetChunk(chunk_id)
   - Storage Server → API Gateway: Stream chunk data
   - API Gateway → Client: Stream to client
6. Complete response
```

### Добавление нового storage сервера

```
1. Start new storage server (storage-7)
2. Storage-7 → PostgreSQL: INSERT INTO storage_servers
3. Storage-7 → PostgreSQL: INSERT 150 rows INTO hash_ring_nodes
4. Storage-7: Start heartbeat loop (every 10s)
5. API Gateway: Refresh hash ring (every 30s)
6. New chunks automatically distributed to storage-7
7. Old chunks remain on original servers (no rebalancing needed)
```

## Производительность

### Ожидаемые характеристики

**Upload:**
- Throughput: ~100-200 MB/s (зависит от сети и дисков)
- Latency: ~50-100ms для метаданных + время передачи файла
- Concurrent uploads: 10-50 (зависит от ресурсов)

**Download:**
- Throughput: ~100-200 MB/s
- Latency: ~50-100ms для метаданных + время передачи файла

**Database:**
- Metadata queries: <10ms
- Hash ring lookup: <1ms (in-memory cache)
- Transaction commit: ~5-10ms

### Узкие места

1. **API Gateway** - single point, все данные проходят через него
   - Митигация: Load balancer + несколько инстансов (future)

2. **PostgreSQL** - single master для записи
   - Митигация: Connection pooling, оптимизация запросов, read replicas

3. **Network bandwidth** - передача больших файлов
   - Митигация: gRPC streaming, compression (опционально)

4. **Disk I/O** на storage серверах
   - Митигация: SSD диски, RAID для производительности

## Безопасность

### Защита данных
- TLS для gRPC соединений между компонентами
- PostgreSQL SSL connections
- SHA-256 checksums для integrity verification
- Валидация размера файлов (max 10 GiB)

### Аутентификация
- API keys для клиентов (базовая)
- Rate limiting для защиты от abuse
- IP whitelisting (опционально)

### Сетевая изоляция
- Docker bridge network
- Только API Gateway exposed наружу (port 8080)
- Storage серверы и PostgreSQL в приватной сети

## Мониторинг

### Метрики API Gateway
- Active uploads/downloads count
- Request latency (p50, p95, p99)
- Throughput (bytes/sec)
- Error rate by type
- Database connection pool stats

### Метрики Storage Servers
- Available disk space
- Chunk count
- gRPC request latency
- I/O errors
- Heartbeat status

### Метрики PostgreSQL
- Connection count
- Query latency
- Transaction rate
- Table/index sizes
- Replication lag (если есть replicas)

### Health Checks
- API Gateway: `GET /health`
- Storage Server: `gRPC HealthCheck()`
- PostgreSQL: `pg_isready`

## Тестирование

### Unit тесты
- Consistent hashing алгоритм (распределение, добавление узлов)
- Chunking logic (разделение файлов)
- Database operations (CRUD)
- gRPC handlers

### Integration тесты
- End-to-end upload/download
- Прерванные загрузки и cleanup
- Добавление новых storage серверов
- Concurrent operations

### Load тесты
- 100 concurrent uploads
- Files размером 10 GiB
- Database performance под нагрузкой
- Storage server throughput

## Развертывание

### Docker Compose
```yaml
services:
  postgres:       # PostgreSQL 15
  api-gateway:    # Go REST API + gRPC client
  storage-1..6:   # Go gRPC servers
```

### Volumes
- `postgres_data` - база данных
- `storage1_data..storage6_data` - chunks на дисках

### Networking
- Bridge network для внутренней коммуникации
- Exposed ports: 8080 (API), 5432 (PostgreSQL для отладки)

## Roadmap

### MVP (текущий scope)
- ✅ Базовая функциональность upload/download
- ✅ Consistent hashing
- ✅ PostgreSQL для метаданных
- ✅ gRPC streaming
- ✅ Обработка прерванных загрузок
- ✅ Docker Compose

### Phase 2: Production Readiness
- PostgreSQL replication (master-slave)
- Automatic failover (Patroni/Stolon)
- Comprehensive monitoring (Prometheus + Grafana)
- Alerting (AlertManager)
- Backup automation

### Phase 3: Scalability
- Multiple API Gateway instances + Load Balancer
- Read replicas для PostgreSQL
- CDN integration
- Compression для chunks

### Phase 4: Advanced Features
- S3-compatible API (multipart upload)
- File versioning
- Access Control Lists (ACLs)
- Metadata search
- Lifecycle policies (auto-delete old files)

## Сравнение с другими вариантами

### vs Вариант 1 (Централизованный PostgreSQL)
**Преимущества Варианта 2:**
- ✅ Consistent hashing для равномерного распределения
- ✅ gRPC streaming для лучшей производительности
- ✅ Более продуманная архитектура для масштабирования

**Недостатки Варианта 2:**
- ❌ Чуть сложнее в реализации
- ❌ Требует in-memory кеш hash ring

### vs Вариант 3 (Микросервисы)
**Преимущества Варианта 2:**
- ✅ Проще в разработке и поддержке
- ✅ Меньше компонентов (нет Consul, отдельных сервисов)
- ✅ Быстрее time-to-market

**Недостатки Варианта 2:**
- ❌ Меньше гибкости для независимого масштабирования
- ❌ API Gateway - single point of failure

## Заключение

Вариант 2 с PostgreSQL представляет собой **оптимальный баланс** между:
- Простотой реализации (быстрый MVP)
- Надежностью (ACID, durability)
- Производительностью (consistent hashing, gRPC)
- Масштабируемостью (легко добавлять storage серверы)

Это решение идеально подходит для:
- ✅ MVP и быстрого старта
- ✅ Production с умеренной нагрузкой
- ✅ Дальнейшего развития в enterprise решение

**Рекомендация:** Начать с этого варианта, собрать метрики в production, и при необходимости эволюционировать в сторону Варианта 3 (микросервисы) или добавлять компоненты по мере роста нагрузки.