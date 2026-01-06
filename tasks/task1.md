# Задача: Разработка тестового покрытия для S3-подобного хранилища

## Контекст

Команда, перед нами стоит задача разработать комплексное тестовое покрытие для распределенной системы хранения файлов с использованием подхода **Test-Driven Development (TDD)**. Система построена на архитектуре с Consistent Hashing, PostgreSQL для метаданных и gRPC для передачи данных.

**Важно:** Мы следуем TDD подходу - сначала пишем тесты, затем реализацию. Каждый тест должен сначала падать (Red), затем мы пишем минимальный код для его прохождения (Green), и только потом рефакторим (Refactor).

## Цели тестирования

1. **Обеспечить надежность** критических компонентов системы
2. **Предотвратить регрессии** при дальнейшей разработке
3. **Документировать поведение** системы через тесты
4. **Упростить рефакторинг** за счет safety net из тестов
5. **Повысить уверенность** в production deployment

## Приоритеты

### P0 (Критично - блокирует релиз)
- Unit тесты для Consistent Hashing
- Unit тесты для Chunking Logic
- Integration тесты для Upload/Download flow
- Database операции (CRUD)

### P1 (Высокий приоритет)
- gRPC handlers тестирование
- Обработка прерванных загрузок
- Concurrent operations
- Error handling и recovery

### P2 (Средний приоритет)
- Load тесты
- Storage server registration/heartbeat
- Cleanup jobs
- Monitoring endpoints

---

## Раздел 1: Unit тесты

### 1.1 Consistent Hashing Algorithm

**Ответственный:** Backend Team  
**Срок:** 3 дня  
**Файл:** `internal/hasher/consistent_hash_test.go`

#### Требования к тестам:

**Test Suite: ConsistentHashRing**

1. **TestNewHashRing_EmptyServers**
   - Создание hash ring без серверов
   - Ожидаемое поведение: пустой ring, но без ошибок
   - Проверка: `ring.GetServer(key)` возвращает ошибку "no servers available"

2. **TestAddServer_SingleServer**
   - Добавление одного сервера с 150 виртуальными узлами
   - Проверка: все 150 узлов созданы
   - Проверка: узлы равномерно распределены по hash space
   - Проверка: `GetServer()` всегда возвращает этот сервер

3. **TestAddServer_MultipleServers**
   - Добавление 6 серверов
   - Проверка: 900 виртуальных узлов (6 * 150)
   - Проверка: каждый сервер имеет ровно 150 узлов
   - Проверка: узлы отсортированы по hash value

4. **TestGetServer_Distribution**
   - Генерация 10,000 случайных ключей
   - Проверка: распределение между серверами ~16.67% ± 2% для каждого
   - Использовать Chi-squared test для статистической проверки
   - Проверка: детерминированность (один ключ → один сервер)

5. **TestGetServer_Deterministic**
   - Один и тот же ключ должен всегда возвращать один сервер
   - Проверка: 1000 вызовов `GetServer("test-key")` → один результат
   - Проверка: после пересоздания ring результат не меняется

6. **TestRemoveServer_Redistribution**
   - Создать ring с 6 серверами
   - Сгенерировать 1000 ключей и запомнить их распределение
   - Удалить один сервер
   - Проверка: только ~16.67% ключей перераспределились
   - Проверка: остальные 83.33% остались на прежних серверах

7. **TestAddServer_MinimalRedistribution**
   - Ring с 6 серверами, 1000 ключей
   - Добавить 7-й сервер
   - Проверка: только ~14.3% ключей перераспределились (1/7)
   - Проверка: новый сервер получил ~14.3% нагрузки

8. **TestHashFunction_xxHash**
   - Проверка корректности xxHash реализации
   - Проверка: известные test vectors дают ожидаемые результаты
   - Проверка: коллизии минимальны (< 0.01% на 100k ключей)

9. **TestVirtualNodes_Count**
   - Проверка влияния количества виртуальных узлов на распределение
   - Тест с 10, 50, 150, 500 узлами
   - Проверка: больше узлов → лучше распределение

10. **TestConcurrentAccess**
    - Concurrent чтение `GetServer()` из 100 goroutines
    - Проверка: нет race conditions (запустить с `-race`)
    - Проверка: все goroutines получают корректные результаты

**Критерии приемки:**
- ✅ Все тесты проходят
- ✅ Code coverage ≥ 90%
- ✅ Нет race conditions (`go test -race`)
- ✅ Benchmark показывает O(log N) для GetServer

---

### 1.2 Chunking Logic

**Ответственный:** Backend Team  
**Срок:** 2 дня  
**Файл:** `internal/chunker/chunker_test.go`

#### Требования к тестам:

**Test Suite: FileChunker**

1. **TestSplitFile_ExactDivision**
   - Файл 6 GiB (6 * 1024^3 bytes)
   - Проверка: 6 chunks по 1 GiB каждый
   - Проверка: сумма размеров chunks = размер файла
   - Проверка: chunk_number от 0 до 5

2. **TestSplitFile_NotExactDivision**
   - Файл 6.5 GiB
   - Проверка: 6 chunks
   - Проверка: первые 5 chunks одинакового размера
   - Проверка: последний chunk содержит остаток
   - Проверка: сумма размеров = размер файла

3. **TestSplitFile_SmallFile**
   - Файл 1 MB (меньше чем 6 chunks)
   - Проверка: все равно 6 chunks
   - Проверка: некоторые chunks могут быть очень маленькими
   - Проверка: корректная обработка

4. **TestSplitFile_MaxSize**
   - Файл 10 GiB (максимальный размер)
   - Проверка: 6 chunks по ~1.67 GiB
   - Проверка: нет переполнения int64
   - Проверка: корректные границы

5. **TestSplitFile_Streaming**
   - Использование io.Reader вместо загрузки в память
   - Проверка: memory usage < 100 MB при обработке 10 GiB файла
   - Проверка: chunks читаются последовательно
   - Использовать memory profiling

6. **TestCalculateChunkBoundaries**
   - Различные размеры файлов: 1KB, 1MB, 1GB, 10GB
   - Проверка: границы chunks корректны
   - Проверка: нет пересечений
   - Проверка: нет пропусков байтов

7. **TestChunkChecksum_SHA256**
   - Вычисление SHA-256 для каждого chunk
   - Проверка: известный файл → известные checksums
   - Проверка: изменение одного байта → другой checksum
   - Проверка: детерминированность

8. **TestReassembleFile**
   - Разделить файл на chunks
   - Собрать обратно
   - Проверка: результат идентичен оригиналу (byte-by-byte)
   - Проверка: checksum совпадает

9. **TestChunkMetadata**
   - Проверка генерации метаданных для каждого chunk
   - Поля: chunk_id, chunk_number, size, checksum, offset
   - Проверка: все поля заполнены корректно

10. **TestErrorHandling_CorruptedChunk**
    - Симуляция поврежденного chunk
    - Проверка: обнаружение через checksum mismatch
    - Проверка: возврат специфичной ошибки

**Критерии приемки:**
- ✅ Все тесты проходят
- ✅ Code coverage ≥ 85%
- ✅ Memory profiling показывает streaming работает
- ✅ Нет memory leaks

---

### 1.3 Database Operations

**Ответственный:** Backend Team  
**Срок:** 3 дня  
**Файл:** `internal/storage/postgres_test.go`

#### Требования к тестам:

**Test Suite: PostgresRepository**

**Setup:** Использовать testcontainers-go для поднятия PostgreSQL в Docker

1. **TestCreateFile_Success**
   - Создание записи в таблице `files`
   - Проверка: file_id сгенерирован (UUID)
   - Проверка: все поля сохранены корректно
   - Проверка: upload_status = 'pending'

2. **TestCreateFile_DuplicateID**
   - Попытка создать файл с существующим file_id
   - Проверка: возвращается ошибка unique constraint violation
   - Проверка: транзакция откатывается

3. **TestCreateChunks_Batch**
   - Создание 6 chunks для одного файла
   - Использовать batch insert
   - Проверка: все 6 записей созданы
   - Проверка: foreign key на file_id корректен
   - Проверка: chunk_number от 0 до 5

4. **TestGetFile_ByID**
   - Получение файла по file_id
   - Проверка: все поля возвращаются
   - Проверка: связанные chunks загружаются (JOIN)

5. **TestGetFile_NotFound**
   - Запрос несуществующего file_id
   - Проверка: возвращается ErrNotFound
   - Проверка: не nil error

6. **TestUpdateFileStatus**
   - Обновление upload_status: pending → uploading → completed
   - Проверка: статус обновляется
   - Проверка: updated_at timestamp обновляется

7. **TestGetChunksByFileID**
   - Получение всех chunks для файла
   - Проверка: возвращается 6 chunks
   - Проверка: отсортированы по chunk_number
   - Проверка: включает storage_server_id

8. **TestTransaction_Rollback**
   - Начать транзакцию
   - Создать файл и chunks
   - Симулировать ошибку
   - Проверка: rollback отменяет все изменения
   - Проверка: БД в консистентном состоянии

9. **TestTransaction_Commit**
   - Создание файла + chunks в одной транзакции
   - Проверка: commit сохраняет все изменения
   - Проверка: данные доступны после commit

10. **TestStorageServerRegistration**
    - Регистрация нового storage сервера
    - Проверка: запись в `storage_servers`
    - Проверка: 150 записей в `hash_ring_nodes`
    - Проверка: hash_value уникальны

11. **TestStorageServerHeartbeat**
    - Обновление last_heartbeat
    - Проверка: timestamp обновляется
    - Проверка: status остается 'active'

12. **TestGetActiveStorageServers**
    - Получение списка активных серверов
    - Проверка: только серверы с last_heartbeat < 30 секунд
    - Проверка: отсортированы по server_id

13. **TestUploadSession_Create**
    - Создание upload session
    - Проверка: session_id сгенерирован
    - Проверка: expires_at = now + 1 hour
    - Проверка: status = 'active'

14. **TestUploadSession_Cleanup**
    - Создать expired session
    - Вызвать cleanup job
    - Проверка: expired sessions удалены
    - Проверка: связанные chunks удалены (CASCADE)

15. **TestConcurrentWrites**
    - 10 goroutines одновременно создают файлы
    - Проверка: нет deadlocks
    - Проверка: все 10 файлов созданы
    - Проверка: нет race conditions

**Критерии приемки:**
- ✅ Все тесты проходят
- ✅ Используется testcontainers для изоляции
- ✅ Code coverage ≥ 85%
- ✅ Тесты проходят с `-race` флагом

---

### 1.4 gRPC Handlers

**Ответственный:** Backend Team  
**Срок:** 2 дня  
**Файл:** `internal/grpc/handlers_test.go`

#### Требования к тестам:

**Test Suite: StorageServiceHandlers**

1. **TestPutChunk_Success**
   - Mock gRPC stream
   - Отправка chunk данных (1 GB)
   - Проверка: chunk сохранен на диск
   - Проверка: checksum вычислен и совпадает
   - Проверка: возвращается success response

2. **TestPutChunk_InvalidChunkID**
   - Отправка с невалидным chunk_id
   - Проверка: возвращается gRPC error INVALID_ARGUMENT

3. **TestPutChunk_DiskFull**
   - Симуляция заполненного диска
   - Проверка: возвращается gRPC error RESOURCE_EXHAUSTED
   - Проверка: частичные данные удалены

4. **TestGetChunk_Success**
   - Запрос существующего chunk
   - Проверка: данные стримятся корректно
   - Проверка: размер совпадает с ожидаемым

5. **TestGetChunk_NotFound**
   - Запрос несуществующего chunk_id
   - Проверка: возвращается gRPC error NOT_FOUND

6. **TestGetChunk_CorruptedFile**
   - Chunk файл поврежден на диске
   - Проверка: checksum mismatch обнаружен
   - Проверка: возвращается gRPC error DATA_LOSS

7. **TestDeleteChunk_Success**
   - Удаление chunk с диска
   - Проверка: файл удален
   - Проверка: success response

8. **TestHealthCheck**
   - Вызов HealthCheck RPC
   - Проверка: возвращается SERVING status
   - Проверка: включает disk space info

9. **TestStreamingPerformance**
   - Benchmark для streaming 1 GB chunk
   - Проверка: throughput > 100 MB/s
   - Проверка: memory usage < 50 MB

10. **TestConcurrentStreams**
    - 10 одновременных PutChunk вызовов
    - Проверка: все успешно завершаются
    - Проверка: нет race conditions

**Критерии приемки:**
- ✅ Все тесты проходят
- ✅ Mock gRPC streams работают корректно
- ✅ Code coverage ≥ 80%
- ✅ Benchmarks показывают приемлемую производительность

---

## Раздел 2: Integration тесты

### 2.1 End-to-End Upload/Download Flow

**Ответственный:** QA + Backend Team  
**Срок:** 4 дня  
**Файл:** `tests/integration/e2e_test.go`

#### Требования к тестам:

**Test Suite: E2E_FileOperations**

**Setup:** Docker Compose с PostgreSQL + API Gateway + 6 Storage Servers

1. **TestUploadDownload_SmallFile**
   - Upload файла 10 MB
   - Проверка: 201 Created, file_id возвращен
   - Download файла по file_id
   - Проверка: содержимое идентично оригиналу
   - Проверка: checksum совпадает

2. **TestUploadDownload_LargeFile**
   - Upload файла 5 GB
   - Проверка: файл разделен на 6 chunks
   - Проверка: chunks распределены по разным storage серверам
   - Download и проверка целостности

3. **TestUploadDownload_MaxSize**
   - Upload файла 10 GB (максимум)
   - Проверка: успешная загрузка
   - Проверка: все chunks на месте
   - Download и проверка

4. **TestUpload_ExceedsMaxSize**
   - Попытка upload файла 11 GB
   - Проверка: возвращается 413 Payload Too Large
   - Проверка: никакие chunks не созданы

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
   - Проверка: GET возвращает 404

9. **TestGetFileMetadata**
   - Upload файла
   - GET /files/{file_id}/metadata
   - Проверка: возвращаются все метаданные
   - Проверка: включает chunk information

10. **TestUploadProgress**
    - Upload большого файла
    - Периодически проверять статус через API
    - Проверка: статус меняется pending → uploading → completed

**Критерии приемки:**
- ✅ Все тесты проходят в Docker Compose окружении
- ✅ Тесты изолированы (cleanup после каждого)
- ✅ Время выполнения < 10 минут
- ✅ Нет flaky tests

---

### 2.2 Interrupted Upload Handling

**Ответственный:** Backend Team  
**Срок:** 2 дня  
**Файл:** `tests/integration/interrupted_upload_test.go`

#### Требования к тестам:

1. **TestInterruptedUpload_ClientDisconnect**
   - Начать upload
   - Симулировать disconnect после 3 chunks
   - Проверка: upload_session создана
   - Подождать TTL (1 час в тесте = 10 секунд)
   - Проверка: cleanup job удалил частичные chunks
   - Проверка: метаданные очищены

2. **TestInterruptedUpload_ServerCrash**
   - Начать upload
   - "Убить" storage сервер во время передачи chunk
   - Проверка: API Gateway обнаруживает ошибку
   - Проверка: транзакция откатывается
   - Проверка: статус файла = 'failed'

3. **TestInterruptedUpload_NetworkTimeout**
   - Симулировать network timeout
   - Проверка: retry logic срабатывает
   - Проверка: после N попыток возвращается ошибка

4. **TestCleanupJob_ExpiredSessions**
   - Создать 5 expired upload sessions
   - Запустить cleanup job
   - Проверка: все 5 sessions удалены
   - Проверка: связанные chunks удалены

5. **TestCleanupJob_ActiveSessions**
   - Создать active upload session
   - Запустить cleanup job
   - Проверка: active session НЕ удалена

**Критерии приемки:**
- ✅ Все тесты проходят
- ✅ Cleanup job работает корректно
- ✅ Нет утечек ресурсов

---

### 2.3 Storage Server Management

**Ответственный:** Backend Team  
**Срок:** 2 дня  
**Файл:** `tests/integration/storage_management_test.go`

#### Требования к тестам:

1. **TestAddStorageServer_Dynamic**
   - Запустить систему с 6 серверами
   - Upload 100 файлов
   - Добавить 7-й storage сервер
   - Проверка: сервер зарегистрирован в БД
   - Проверка: hash ring обновлен
   - Upload еще 100 файлов
   - Проверка: новый сервер получает ~14% chunks

2. **TestRemoveStorageServer**
   - Остановить один storage сервер
   - Проверка: heartbeat прекращается
   - Проверка: сервер помечен как 'inactive'
   - Проверка: новые chunks не направляются на него
   - Проверка: существующие chunks остаются доступны

3. **TestStorageServerFailover**
   - Upload файла
   - "Убить" storage сервер с одним из chunks
   - Попытка download
   - Проверка: возвращается ошибка (нет репликации в MVP)
   - Проверка: система остается стабильной

4. **TestHeartbeatMechanism**
   - Запустить storage сервер
   - Проверка: heartbeat отправляется каждые 10 секунд
   - Остановить heartbeat
   - Проверка: через 30 секунд сервер 'inactive'

5. **TestHashRingRefresh**
   - API Gateway кеширует hash ring
   - Добавить новый storage сервер
   - Проверка: через 30 секунд API Gateway обновил ring
   - Проверка: новые chunks направляются на новый сервер

**Критерии приемки:**
- ✅ Все тесты проходят
- ✅ Динамическое добавление серверов работает
- ✅ Heartbeat механизм надежен

---

## Раздел 3: Concurrent Operations

**Ответственный:** Backend Team  
**Срок:** 2 дня  
**Файл:** `tests/integration/concurrent_test.go`

### Требования к тестам:

1. **TestConcurrentUploads**
   - 50 goroutines одновременно загружают файлы
   - Проверка: все 50 файлов успешно загружены
   - Проверка: нет race conditions
   - Проверка: нет deadlocks в БД

2. **TestConcurrentDownloads**
   - Upload 10 файлов
   - 100 goroutines одновременно скачивают их
   - Проверка: все downloads успешны
   - Проверка: данные корректны

3. **TestMixedOperations**
   - Одновременно: uploads, downloads, deletes, list
   - Проверка: система остается консистентной
   - Проверка: нет data corruption

4. **TestDatabaseConnectionPool**
   - 100 одновременных запросов к БД
   - Проверка: connection pool не исчерпывается
   - Проверка: нет connection leaks

5. **TestRaceConditions**
   - Запустить все тесты с `-race` флагом
   - Проверка: нет race conditions обнаружено

**Критерии приемки:**
- ✅ Все тесты проходят с `-race`
- ✅ Нет deadlocks
- ✅ Система стабильна под нагрузкой

---

## Раздел 4: Load Testing

**Ответственный:** QA Team  
**Срок:** 3 дня  
**Инструмент:** k6 или Locust

### Сценарии нагрузочного тестирования:

1. **LoadTest_SustainedUpload**
   - 100 concurrent users
   - Каждый загружает файлы 1-5 GB
   - Длительность: 10 минут
   - Метрики:
     - Throughput (MB/s)
     - Request latency (p50, p95, p99)
     - Error rate
     - Database connection count

2. **LoadTest_SustainedDownload**
   - Pre-upload 1000 файлов
   - 200 concurrent users скачивают
   - Длительность: 10 минут
   - Метрики: аналогично upload

3. **LoadTest_MixedWorkload**
   - 50% uploads, 30% downloads, 20% list/metadata
   - 150 concurrent users
   - Длительность: 15 минут

4. **LoadTest_SpikeTest**
   - Резкий скачок с 10 до 200 users
   - Проверка: система справляется
   - Проверка: нет cascading failures

5. **LoadTest_StressTest**
   - Постепенное увеличение нагрузки до failure point
   - Определить максимальную пропускную способность
   - Определить bottlenecks

**Критерии приемки:**
- ✅ Throughput ≥ 100 MB/s
- ✅ p95 latency < 5 секунд для 1 GB файла
- ✅ Error rate < 1%
- ✅ Система восстанавливается после spike

---

## Раздел 5: Error Handling & Edge Cases

**Ответственный:** Backend Team  
**Срок:** 2 дня  
**Файл:** `tests/integration/error_handling_test.go`

### Требования к тестам:

1. **TestDatabaseConnectionLoss**
   - Остановить PostgreSQL во время операции
   - Проверка: graceful error handling
   - Проверка: retry logic
   - Восстановить PostgreSQL
   - Проверка: система восстанавливается

2. **TestStorageServerConnectionLoss**
   - Потеря связи со storage сервером
   - Проверка: timeout обрабатывается
   - Проверка: возвращается понятная ошибка клиенту

3. **TestDiskFullOnStorageServer**
   - Симулировать заполненный диск
   - Проверка: chunk не сохраняется
   - Проверка: возвращается RESOURCE_EXHAUSTED
   - Проверка: транзакция откатывается

4. **TestCorruptedChunkDetection**
   - Вручную изменить chunk файл на диске
   - Попытка download
   - Проверка: checksum mismatch обнаружен
   - Проверка: возвращается DATA_LOSS error

5. **TestInvalidInputValidation**
   - Пустой filename
   - Негативный file size
   - Невалидный UUID
   - Проверка: все возвращают 400 Bad Request

**Критерии приемки:**
- ✅ Все error cases обработаны gracefully
- ✅ Нет panics
- ✅ Понятные error messages для клиентов

---

## Раздел 6: Monitoring & Health Checks

**Ответственный:** DevOps + Backend Team  
**Срок:** 2 дня  
**Файл:** `tests/integration/monitoring_test.go`

### Требования к тестам:

1. **TestHealthEndpoint**
   - GET /health
   - Проверка: возвращает 200 OK
   - Проверка: включает status всех компонентов

2. **TestMetricsEndpoint**
   - GET /metrics (Prometheus format)
   - Проверка: метрики экспортируются
   - Проверка: включает custom metrics

3. **TestStorageServerHealth**
   - gRPC HealthCheck для каждого storage сервера
   - Проверка: все возвращают SERVING
   - Проверка: включает disk space info

4. **TestDatabaseHealth**
   - Проверка pg_isready
   - Проверка: connection pool stats

5. **TestAlerting**
   - Симулировать критическую ситуацию
   - Проверка: алерт генерируется (если настроено)

**Критерии приемки:**
- ✅ Health checks работают
- ✅ Метрики корректны
- ✅ Мониторинг покрывает все компоненты

---

## Инфраструктура тестирования

### Требования к окружению:

1. **CI/CD Pipeline**
   - GitHub Actions или GitLab CI
   - Автоматический запуск тестов на каждый PR
   - Блокировка merge при failing tests

2. **Test Containers**
   - PostgreSQL в Docker для unit тестов
   - Полный Docker Compose для integration тестов
   - Изо