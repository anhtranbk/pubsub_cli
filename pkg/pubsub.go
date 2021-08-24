package pkg

import (
	"context"
	"fmt"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/pkg/errors"
	"github.com/rs/xid"
	"google.golang.org/api/option"
)

// PubSubClient represents extended pubsub client
type PubSubClient struct {
	*pubsub.Client
}

// NewPubSubClient initializes new pubsub client
func NewPubSubClient(ctx context.Context, projectID, pubsubEmulatorHost, gcpCredFilePath string) (*PubSubClient, error) {
	if projectID == "" {
		return nil, errors.New("GCP Project ID must be set from either env variable 'GCP_PROJECT_ID' or --project flag")
	}

	var opts []option.ClientOption
	if pubsubEmulatorHost != "" {
		conn, err := grpc.DialContext(ctx, pubsubEmulatorHost, grpc.WithInsecure())
		if err != nil {
			return nil, errors.Wrap(err, "grpc.Dial")
		}
		opts = append(opts, option.WithGRPCConn(conn))
	} else {
		opts = append(opts, option.WithCredentialsFile(gcpCredFilePath))
	}

	client, err := pubsub.NewClient(ctx, projectID, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "create new pubsub client")
	}
	return &PubSubClient{client}, nil
}

func (pc *PubSubClient) FindAllTopics(ctx context.Context) ([]*pubsub.Topic, error) {
	var topics []*pubsub.Topic
	topicIterator := pc.Topics(ctx)
	for {
		topic, err := topicIterator.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, err
		}
		topics = append(topics, topic)
	}
	return topics, nil
}

// FindTopic finds the topic or return nil if not exists.
func (pc *PubSubClient) FindTopic(ctx context.Context, topicID string) (*pubsub.Topic, error) {
	topic := pc.Topic(topicID)

	exists, err := topic.Exists(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "find topic %s", topicID)
	}
	if exists {
		return topic, nil
	}
	return nil, nil
}

// FindTopics finds the given topics.
// returned topics are unordered
func (pc *PubSubClient) FindTopics(ctx context.Context, topicIDs []string) ([]*pubsub.Topic, error) {
	var topics []*pubsub.Topic
	eg := errgroup.Group{}
	topicChan := make(chan *pubsub.Topic, len(topicIDs))
	for _, topicID := range topicIDs {
		topicID := topicID
		eg.Go(func() error {
			topic, err := pc.FindTopic(ctx, topicID)
			if err != nil {
				return err
			}
			if topic != nil {
				topicChan <- topic
			}
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	close(topicChan)

	for topic := range topicChan {
		topics = append(topics, topic)
	}
	return topics, nil
}

// FindOrCreateTopic finds the topic or create if not exists.
func (pc *PubSubClient) FindOrCreateTopic(ctx context.Context, topicID string) (*pubsub.Topic, error) {
	topic, err := pc.FindTopic(ctx, topicID)
	if err != nil {
		return nil, err
	}
	if topic != nil {
		return topic, nil
	}

	topic, err = pc.CreateTopic(ctx, topicID)
	if err != nil {
		return nil, errors.Wrapf(err, "create topic %s", topicID)
	}
	return topic, nil
}

// FindOrCreateTopics finds the given topics or creates if not exists.
// returned topics are unordered
func (pc *PubSubClient) FindOrCreateTopics(ctx context.Context, topicIDs []string) ([]*pubsub.Topic, error) {
	var topics []*pubsub.Topic
	eg := errgroup.Group{}
	topicChan := make(chan *pubsub.Topic, len(topicIDs))
	for _, topicID := range topicIDs {
		topicID := topicID
		eg.Go(func() error {
			topic, err := pc.FindOrCreateTopic(ctx, topicID)
			if err != nil {
				return errors.Wrapf(err, "find or create topic %s", topicID)
			}
			topicChan <- topic
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	close(topicChan)

	for topic := range topicChan {
		topics = append(topics, topic)
	}
	return topics, nil
}

// CreateUniqueSubscription creates an unique subscription to given topic
func (pc *PubSubClient) CreateUniqueSubscription(ctx context.Context, topic *pubsub.Topic, ackDeadline time.Duration) (*pubsub.Subscription, error) {
	subscriptionConfig := pubsub.SubscriptionConfig{
		Topic:            topic,
		AckDeadline:      ackDeadline,
		ExpirationPolicy: 24 * time.Hour,
		Labels:           map[string]string{"created_by": "pubsub_cli"},
	}
	sub, err := pc.CreateSubscription(ctx, fmt.Sprintf("pubsub_cli_%s", xid.New().String()), subscriptionConfig)
	if err != nil {
		return nil, err
	}
	return sub, err
}
