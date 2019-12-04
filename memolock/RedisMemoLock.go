package memolock

import (
	"errors"
	"time"

	"github.com/go-redis/redis"
	"github.com/gofrs/uuid"

	"fmt"
	"sync"
)

// FetchFunc is the function that the caller should provide to compute the value if not present in Redis already.
// time.Duration defines for how long the value should be cached in Redis.
type FetchFunc = func() (string, time.Duration, error)

// ErrClosing happens when calling Close(), all pending requests will be failed with this error
var ErrClosing = errors.New("operation canceled by Close()")

type subRequest struct {
	name    string
	isUnsub bool
	resCh   chan string
}

// RedisMemoLock implements the "promise" mechanism
type RedisMemoLock struct {
	client        *redis.Client
	resourceTag   string
	subCh         chan subRequest
	notifCh       <-chan *redis.Message
	subscriptions map[string][]chan string
	mu            *sync.Mutex
}

func (r *RedisMemoLock) dispatch() {
	for {
		select {
		// We have a new sub/unsub request
		case sub, ok := <-r.subCh:
			if !ok {
				// We are closing, close all pending channels
				for _, list := range r.subscriptions {
					for _, ch := range list {
						close(ch)
					}
				}
				return
			}
			switch sub.isUnsub {
			case false:
				list, _ := r.subscriptions[sub.name]
				r.subscriptions[sub.name] = append(list, sub.resCh)
			case true:
				if list, ok := r.subscriptions[sub.name]; ok {
					newList := list[:0]
					for _, x := range list {
						if sub.resCh != x {
							newList = append(newList, x)
						}
					}
					for i := len(newList); i < len(list); i++ {
						list[i] = nil
					}
					r.subscriptions[sub.name] = list
				}
			}
			// We have a new notification from Redis Pub/Sub
		case msg := <-r.notifCh:
			if listeners, ok := r.subscriptions[msg.Channel]; ok {
				for _, ch := range listeners {
					if ch != nil {

						ch <- msg.Payload
						close(ch)
					}
				}
				r.subscriptions[msg.Channel] = nil
			}
		}
	}
}

// NewRedisMemoLock Creates a new RedisMemoLock instance
func NewRedisMemoLock(client *redis.Client, resourceTag string) (*RedisMemoLock, error) {
	pattern := resourceTag + "/notif:*"

	pubsub := client.PSubscribe(pattern)
	_, err := pubsub.Receive()
	if err != nil {
		return nil, err
	}

	result := RedisMemoLock{
		client:        client,
		resourceTag:   resourceTag,
		subCh:         make(chan subRequest),
		notifCh:       pubsub.Channel(),
		subscriptions: make(map[string][]chan string),
		mu:            &sync.Mutex{},
	}
	// Start the dispatch loop
	go result.dispatch()

	return &result, nil
}

// Close stops listening to Pub/Sub and resolves all pending subscriptions with ErrClosing.
func (r *RedisMemoLock) Close() {
	close(r.subCh)
}

// GetResource tries to get a resource from Redis, resorting to call generatingFunc in case of a cache miss
func (r *RedisMemoLock) GetResource(resID string, timeout time.Duration, generatingFunc FetchFunc) (string, error) {
	reqUUID := uuid.Must(uuid.NewV4()).String()
	return r.getResourceImpl(resID, generatingFunc, timeout, reqUUID, false)
}

func (r *RedisMemoLock) getResourceImpl(resID string, generatingFunc FetchFunc, timeout time.Duration, reqUUID string, externallyManaged bool) (string, error) {

	resourceID := r.resourceTag + ":" + resID
	lockID := r.resourceTag + "/lock:" + resID
	notifID := r.resourceTag + "/notif:" + resID

	// If the resource is available, return it immediately.
	res, err := r.client.Get(resourceID).Result()
	if err != redis.Nil { // key is not missing
		if err != nil { // real error happened?
			return "", err
		}
		//Counter <- "FromBase_"+resID
		return res, nil
	}
	// key is missing

	// The resource is not available, can we get the lock?
	resourceLock, err := r.client.SetNX(lockID, reqUUID, timeout).Result()
	if err != nil {
		return "", err
	}

	if resourceLock {
		// We acquired the lock, use the client-provided func to generate the resource.
		resourceValue, resourceTTL, err := generatingFunc()
		if err != nil {
			return "", err
		}

		if !externallyManaged {
			// Storage of the token on Redis and notification is handled
			// by us and we can return the token immediately.
			r.mu.Lock()
			pipe := r.client.Pipeline()
			pipe.Set(resourceID, resourceValue, resourceTTL)
			pipe.Publish(notifID, resourceValue)
			_, err := pipe.Exec()
			r.mu.Unlock()
			if err != nil {
				return "", err
			}
			return resourceValue, nil
		}

		// The notification will be created by an external system
		// so we falltrough and subscribe to notifs anyway.
	}

	// The resource is not ready yet so we wait for a notification of completion.
	subReq := subRequest{name: notifID, isUnsub: false, resCh: make(chan string, 1)}

	// resCh needs to have buffering to make sure it does not
	// lock the dispatch goroutine when timeouts happen.

	// Send a request to sub
	r.subCh <- subReq
	unsubRequest := subRequest{name: notifID, isUnsub: true, resCh: subReq.resCh}

	// Refetch the key in case we missed the pubsub announcement by a hair.
	// subCh must have no buffering to make sure we do this *after* the sub
	// really takes effect.
	res, err = r.client.Get(resourceID).Result()
	if err != redis.Nil { // key is not missing
		if err != nil { // real error happened?
			fmt.Println("error found", res, err)
			r.subCh <- unsubRequest
			return "", err
		}
		r.subCh <- unsubRequest

		return res, nil
	}
	// key is missing
	select {
	case res, ok := <-subReq.resCh:
		if !ok {
			return "", ErrClosing
		}
		return res, nil
	}
}

