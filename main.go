package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"text/template"
	"time"

	"context"

	"github.com/IBM/sarama"
	"github.com/go-redis/redis/v8"
	"github.com/gorilla/mux"
)

type Restaurant struct {
	RestaurantID string `json:"restaurantId"`
	Name         string `json:"name"`
}

type Menu struct {
	RestaurantID string  `json:"restaurantId"`
	MenuID       string  `json:"menuId"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	Price        float64 `json:"price"`
}

type Rider struct {
	RiderID string `json:"riderId"`
	Name    string `json:"name"`
}

type OrderItem struct {
	MenuID   string `json:"menu_id"`
	Quantity int    `json:"quantity"`
}

type PlaceOrderRequest struct {
	RestaurantID string      `json:"restaurant_id"`
	Items        []OrderItem `json:"items"`
}

type PlaceOrderResponse struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
}

type AcceptOrderRequest struct {
	OrderID      string `json:"order_id"`
	RestaurantID string `json:"restaurant_id"`
}

type AcceptOrderResponse struct {
	Status string `json:"status"`
}

type PickupConfirmationRequest struct {
	OrderID string `json:"order_id"`
	RiderID string `json:"rider_id"`
}

type PickupConfirmationResponse struct {
	Status string `json:"status"`
}

type DeliverOrderRequest struct {
	OrderID string `json:"order_id"`
	RiderID string `json:"rider_id"`
}

type DeliverOrderResponse struct {
	Status string `json:"status"`
}

type NotificationRequest struct {
	Recipient string `json:"recipient"`
	OrderID   string `json:"order_id"`
	Message   string `json:"message"`
}

type NotificationResponse struct {
	Status string `json:"status"`
}

var (
	restaurants []Restaurant
	menus       []Menu
	riders      []Rider
	mu          sync.RWMutex
	redisClient *redis.Client
	kafkaClient sarama.ConsumerGroup
	ctx         = context.Background()
)

func initRedis() {
	redisClient = redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}
	log.Println("Connected to Redis")
}

func initKafka() {
	config := sarama.NewConfig()
	config.Producer.Return.Successes = true
	config.Version = sarama.V2_8_0_0 // Match your Kafka cluster version
	config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRoundRobin
	config.Consumer.Offsets.Initial = sarama.OffsetOldest

	client, err := sarama.NewConsumerGroup([]string{"localhost:9093"}, "order_service_group", config)
	if err != nil {
		log.Fatalf("Failed to start Kafka consumer group: %v", err)
	}

	kafkaClient = client
	log.Println("Connected to Kafka")
}

func initKafkaConsumer(groupID string, topics []string) {

	go func() {
		for {
			if err := kafkaClient.Consume(ctx, topics, &kafkaConsumerHandler{}); err != nil {
				log.Printf("Error consuming Kafka messages: %v", err)
				time.Sleep(1 * time.Second)
			}
		}
	}()

	log.Printf("Kafka consumer group '%s' started on topics: %v", groupID, topics)
}

type kafkaConsumerHandler struct{}

func (h *kafkaConsumerHandler) Setup(_ sarama.ConsumerGroupSession) error {
	return nil
}

func (h *kafkaConsumerHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

func (h *kafkaConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		log.Printf("Message received: topic=%s partition=%d offset=%d key=%s value=%s",
			msg.Topic, msg.Partition, msg.Offset, string(msg.Key), string(msg.Value))

		switch msg.Topic {
		case "order_created":
			log.Printf("Order created: %+v", msg.Value)
			processOrderCreated(msg.Value)
		case "order_accepted":
			processOrderAccepted(msg.Value)
		case "order_picked_up":
			processOrderPickedUp(msg.Value)
		case "order_delivered":
			processOrderDelivered(msg.Value)
		default:
			log.Printf("Unknown topic: %s", msg.Topic)
		}

		// Mark message as processed
		session.MarkMessage(msg, "")
	}
	return nil
}

func processOrderCreated(value []byte) {
	var order PlaceOrderRequest
	if err := json.Unmarshal(value, &order); err != nil {
		log.Printf("Error unmarshalling order_created message: %v", err)
		return
	}
	log.Printf("Order created: %+v", order)
}

func processOrderAccepted(value []byte) {
	var acceptReq AcceptOrderRequest
	if err := json.Unmarshal(value, &acceptReq); err != nil {
		log.Printf("Error unmarshalling order_accepted message: %v", err)
		return
	}
	log.Printf("Order accepted: %+v", acceptReq)

	// Add business logic for order acceptance
}

func processOrderPickedUp(value []byte) {
	var pickupReq PickupConfirmationRequest
	if err := json.Unmarshal(value, &pickupReq); err != nil {
		log.Printf("Error unmarshalling order_picked_up message: %v", err)
		return
	}
	log.Printf("Order picked up: %+v", pickupReq)

	// Add business logic for order pickup
}

func processOrderDelivered(value []byte) {
	var deliverReq DeliverOrderRequest
	if err := json.Unmarshal(value, &deliverReq); err != nil {
		log.Printf("Error unmarshalling order_delivered message: %v", err)
		return
	}
	log.Printf("Order delivered: %+v", deliverReq)
}

func loadData(fileName string, dest interface{}) error {
	data, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("error reading file %s: %w", fileName, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("error unmarshalling file %s: %w", fileName, err)
	}
	return nil
}

func loadJSONData() {
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		if err := loadData("./data/restaurants.json", &restaurants); err != nil {
			log.Printf("Failed to load restaurants: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := loadData("./data/menus.json", &menus); err != nil {
			log.Printf("Failed to load menus: %v", err)
		}
	}()

	go func() {
		defer wg.Done()
		if err := loadData("./data/riders.json", &riders); err != nil {
			log.Printf("Failed to load riders: %v", err)
		}
	}()

	wg.Wait()
	log.Println("All JSON data loaded successfully")
}

func cacheData(key string, data interface{}) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		log.Printf("Error marshalling data for cache: %v", err)
		return
	}
	if err := redisClient.Set(ctx, key, dataBytes, 24*time.Hour).Err(); err != nil {
		log.Printf("Error caching data in Redis: %v", err)
	}
}

func getCachedData(key string, dest interface{}) bool {
	data, err := redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return false
	} else if err != nil {
		log.Printf("Error fetching data from Redis: %v", err)
		return false
	}
	if err := json.Unmarshal([]byte(data), dest); err != nil {
		log.Printf("Error unmarshalling cached data: %v", err)
		return false
	}
	return true
}

func viewRestaurantHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Fetching restaurants...")
	mu.RLock()
	defer mu.RUnlock()

	var cachedRestaurants []Restaurant
	if getCachedData("restaurants", &cachedRestaurants) {
		log.Println("Serving restaurants from Redis cache")
		json.NewEncoder(w).Encode(cachedRestaurants)
		return
	}

	log.Println("Serving restaurants from memory")
	cacheData("restaurants", restaurants)
	json.NewEncoder(w).Encode(restaurants)
}

func viewMenuHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Fetching menu...")
	restaurantID := r.URL.Query().Get("restaurantId")
	if restaurantID == "" {
		http.Error(w, "Missing restaurantId query parameter", http.StatusBadRequest)
		return
	}

	var cachedMenus []Menu
	cacheKey := fmt.Sprintf("menu:%s", restaurantID)
	if getCachedData(cacheKey, &cachedMenus) {
		log.Println("Serving menu from Redis cache")
		json.NewEncoder(w).Encode(cachedMenus)
		return
	}

	var filteredMenus []Menu
	mu.RLock()
	for _, menu := range menus {
		if menu.RestaurantID == restaurantID {
			filteredMenus = append(filteredMenus, menu)
		}
	}
	mu.RUnlock()

	cacheData(cacheKey, filteredMenus)
	json.NewEncoder(w).Encode(filteredMenus)
}

func viewRiderHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Fetching riders...")
	mu.RLock()
	defer mu.RUnlock()

	var cachedRiders []Rider
	if getCachedData("restaurants", &cachedRiders) {
		log.Println("Serving riders from Redis cache")
		json.NewEncoder(w).Encode(cachedRiders)
		return
	}

	log.Println("Serving riders from memory")
	cacheData("riders", riders)
	json.NewEncoder(w).Encode(riders)
}

func placeOrderHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Placing order...")
	var orderReq PlaceOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&orderReq); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	orderID := fmt.Sprintf("order-%d", time.Now().UnixNano())
	cacheKey := fmt.Sprintf("order:%s", orderID)
	cacheData(cacheKey, orderReq)

	initKafkaConsumer("order_service_group", []string{"order_created"})

	json.NewEncoder(w).Encode(PlaceOrderResponse{OrderID: orderID, Status: "created"})
}

func acceptOrderHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Accept order...")
	var acceptReq AcceptOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&acceptReq); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	cacheKey := fmt.Sprintf("order:%s", acceptReq.OrderID)
	cacheData(cacheKey, acceptReq)

	initKafkaConsumer("order_service_group", []string{"order_accepted"})

	json.NewEncoder(w).Encode(AcceptOrderResponse{Status: "accepted"})
}

func pickupOrderHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Pickup order called")
	var pickupReq PickupConfirmationRequest
	if err := json.NewDecoder(r.Body).Decode(&pickupReq); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	initKafkaConsumer("order_service_group", []string{"order_picked_up"})

	json.NewEncoder(w).Encode(PickupConfirmationResponse{Status: "picked_up"})
}

func deliverOrderHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Deliver order called")
	var deliverReq DeliverOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&deliverReq); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	initKafkaConsumer("order_service_group", []string{"order_delivered"})

	json.NewEncoder(w).Encode(DeliverOrderResponse{Status: "delivered"})
}

func sendNotificationHandler(w http.ResponseWriter, r *http.Request) {
	log.Println("Send notification called")
	var notificationReq NotificationRequest
	if err := json.NewDecoder(r.Body).Decode(&notificationReq); err != nil {
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	log.Printf("Notification sent: Recipient: %s, OrderID: %s, Message: %s",
		notificationReq.Recipient, notificationReq.OrderID, notificationReq.Message)

	json.NewEncoder(w).Encode(NotificationResponse{Status: "sent"})
}

func renderTemplate(w http.ResponseWriter, page string) {
	t, err := template.ParseFiles(page)
	if err != nil {
		log.Println(err)
		return
	}

	err = t.Execute(w, nil)
	if err != nil {
		log.Println(err)
		return
	}
}

func homePage(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, "index.html")
}

func main() {
	initRedis()
	initKafka()
	defer kafkaClient.Close()
	loadJSONData()

	go initKafkaConsumer("order_service_group", []string{"order_created", "order_accepted", "order_picked_up", "order_delivered"})

	r := mux.NewRouter()
	r.HandleFunc("/", homePage)
	r.HandleFunc("/restaurant", viewRestaurantHandler).Methods("GET")
	r.HandleFunc("/menu", viewMenuHandler).Methods("GET") // Requires "restaurantId" query param
	r.HandleFunc("/rider", viewRiderHandler).Methods("GET")
	r.HandleFunc("/order", placeOrderHandler).Methods("POST")
	r.HandleFunc("/restaurant/order/accept", acceptOrderHandler).Methods("POST")
	r.HandleFunc("/rider/order/pickup", pickupOrderHandler).Methods("POST")
	r.HandleFunc("/rider/order/deliver", deliverOrderHandler).Methods("POST")
	r.HandleFunc("/notification/send", sendNotificationHandler).Methods("POST")

	log.Println("Server starting on port 8080")
	log.Fatal(http.ListenAndServe(":8080", r))
}
