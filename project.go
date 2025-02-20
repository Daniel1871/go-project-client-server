package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/time/rate"
)

type Summary struct {
	Positive int `json:"positive"`
	Negative int `json:"negative"`
}

type Stats struct {
	Server  Summary            `json:"server"`
	Clients map[string]Summary `json:"clients"`
}

var (
	stats       = Stats{Clients: make(map[string]Summary)}
	statsMu     sync.Mutex
	workerMu    sync.Mutex
	clientsDone = make(chan struct{}, 2)
)

func main() {
	// Загрузка переменных окружения из файла .env
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	// Получение значения переменной окружения "PORT"
	port := os.Getenv("PORT")
	if port == "" {
		log.Fatal("PORT environment variable is not set")
	}

	// Инициализация обработчиков
	http.Handle("/", rateLimitMiddleware(http.HandlerFunc(handleRequest)))
	http.HandleFunc("/stats", statsHandler)
	http.HandleFunc("/health", healthHandler)

	// Запуск сервера
	serverReady := make(chan struct{})
	go func() {
		log.Printf("Server is running on port %s\n", port)
		close(serverReady) // Сигнализируем о готовности сервера
		log.Fatal(http.ListenAndServe(":"+port, nil))
	}()
	<-serverReady // Ждем запуска сервера (т.е. закрытия канала в горутине)

	// Запуск клиентов
	wg := sync.WaitGroup{}
	wg.Add(3)
	go client("client1", &wg)
	go client("client2", &wg)
	go checkServer("Client3", &wg)
	wg.Wait()
}

func generateStatus() int {
	// 70% положительных, 30% отрицательных
	if rand.Intn(10) < 7 {
		if rand.Intn(2) == 0 {
			return http.StatusOK
		}
		return http.StatusAccepted
	}
	if rand.Intn(2) == 0 {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

func statsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid Method", http.StatusMethodNotAllowed)
		return
	}

	// Устанавливаем заголовок Content-Type для JSON
	w.Header().Set("Content-Type", "application/json")

	// Блокируем доступ к структуре stats для предотвращения гонок данных
	statsMu.Lock()
	defer statsMu.Unlock()

	// Сериализуем JSON
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// Middleware для ограничения скорости обработки запросов
func rateLimitMiddleware(next http.Handler) http.Handler {
	// Создаем rate.Limiter: 5 запросов в секунду, буфер размером 5
	limiter := rate.NewLimiter(rate.Limit(5), 5)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Проверяем, доступен ли токен
		if !limiter.Allow() {
			w.WriteHeader(http.StatusTooManyRequests) // Лимит превышен
			return
		}
		// Передаем управление следующему обработчику
		next.ServeHTTP(w, r)
	})
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	// Проверяем метод запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid Method", http.StatusMethodNotAllowed)
		return
	}

	// Проверяем имя клиента из заголовка
	clientName := r.Header.Get("X-Client-Name")             // Получаем значение заголовка
	if clientName != "client1" && clientName != "client2" { // По условию только клиенты 1 и 2 получают статусы
		http.Error(w, "Access denied: only client1 and client2 are allowed", http.StatusForbidden)
		return
	}

	// Генерация рандомного статуса
	status := generateStatus()

	// Подсчет количества статусов для сервера
	statsMu.Lock() // Защита доступа к структуре от гонки за ее данными
	if status == http.StatusOK || status == http.StatusAccepted {
		stats.Server.Positive++
	} else {
		stats.Server.Negative++
	}
	statsMu.Unlock()

	// Отправка статуса ответа
	w.WriteHeader(status)
}

func client(name string, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { clientsDone <- struct{}{} }()

	clientStats := make(map[int]int) // Мапа для хранения статистики по статусам для данного клиента

	// Создаем rate.Limiter для ограничения скорости: 5 запросов в секунду
	limiter := rate.NewLimiter(rate.Limit(5), 5)

	var workerWg sync.WaitGroup // WaitGroup для синхронизации воркеров

	for i := 0; i < 2; i++ { // Запускаем 2 воркера (отдельные горутины)
		workerWg.Add(1)
		go worker(name, &workerWg, clientStats, limiter)
	}
	workerWg.Wait()

	// Вывод статистики клиента
	fmt.Printf("\n=== %s statistics ===\n", name)
	total := 0
	for status, count := range clientStats {
		fmt.Printf("Status %d: %d requests\n", status, count)
		total += count
	}
	fmt.Printf("Total Requests: %d\n", total)
}

func worker(name string, workerWg *sync.WaitGroup, clientStats map[int]int, limiter *rate.Limiter) {
	defer workerWg.Done()     // Уменьшаем счетчик WaitGroup после завершения
	for i := 0; i < 50; i++ { // Каждый воркер отправляет 50 POST-запросов
		if i%5 == 0 {
			workerMu.Lock()
		}

		if err := limiter.Wait(context.Background()); err != nil {
			log.Printf("[%s] Rate limiter error: %v\n", name, err)
			// Гарантируем разблокировку мьютекса в случае ошибки
			if i%5 == 4 {
				workerMu.Unlock()
			}
			continue
		}

		req, err := http.NewRequest("POST", "http://localhost:"+os.Getenv("PORT"), nil)
		if err != nil {
			log.Printf("[%s] Error creating request: %v\n", name, err)
			if i%5 == 4 {
				workerMu.Unlock()
			}
			continue
		}

		req.Header.Set("X-Client-Name", name)   // Устанавливаем заголовок со значением name
		resp, err := http.DefaultClient.Do(req) // Отправка запроса на сервер
		if err != nil {
			log.Printf("[%s] Error sending request: %v\n", name, err)
			if i%5 == 4 {
				workerMu.Unlock()
			}
			continue
		}

		// Закрываем тело ответа
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[%s] Error closing response body: %v\n", name, closeErr)
		}

		// Обновляем статистику
		statsMu.Lock() // Защита доступа к структуре от гонки за ее данными
		clientStats[resp.StatusCode]++

		clientSummary := stats.Clients[name]
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
			clientSummary.Positive++
		} else if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusInternalServerError {
			clientSummary.Negative++
		}
		stats.Clients[name] = clientSummary
		statsMu.Unlock()

		if i%5 == 4 {
			workerMu.Unlock()
		}
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid Method", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func checkServer(name string, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(5 * time.Second) // Создаём тикер с интервалом 5 секунд
	defer ticker.Stop()

	afterIter := 0 // Счетчик итераций после окончания работы клиентов

	for range ticker.C { // Тикер будет срабатывать каждые 5 секунд
		if len(clientsDone) == 2 {
			if afterIter == 6 { // После работы клиентов допускаем 6 итераций (по 5 секунд) => 30 секунд
				close(clientsDone)
				return
			}
			afterIter++
		}

		resp, err := http.Get("http://localhost:" + os.Getenv("PORT") + "/health")
		if err != nil { // Ошибка при отправке GET запроса на /health <=> сервер недоступен
			log.Printf("[%s] Server unavailable\n", name)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			// Закрываем тело ответа перед выходом
			if closeErr := resp.Body.Close(); closeErr != nil {
				log.Printf("[%s] Error closing response body: %v\n", name, closeErr)
			}
			log.Printf("[%s] Server available but returned non-OK status: %d\n", name, resp.StatusCode)
			continue
		}
		// Закрываем тело ответа (освобождаем ресурсы, связанные с http-ответом)
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("[%s] Error closing response body: %v\n", name, closeErr)
		}
		log.Printf("[%s] Server available\n", name)
	}
}
