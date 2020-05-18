package cmd

import (
	"cloud.google.com/go/pubsub"
	"context"
	"fmt"
	"github.com/k-yomo/pubsub_cli/pkg"
	"github.com/mitchellh/colorstring"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"io"
)

// newSubscribeCmd returns the command to subscribe messages
func newSubscribeCmd(out io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:     "subscribe TOPIC_ID ...",
		Short:   "subscribe Pub/Sub topics",
		Long:    "create subscription for given Pub/Sub topic and subscribe the topic",
		Example: "pubsub_cli subscribe test_topic another_topic --host=localhost:8085 --project=test_project",
		Aliases: []string{"s"},
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			topicIDs := args
			projectID, err := cmd.Flags().GetString(projectFlagName)
			if err != nil {
				return err
			}
			emulatorHost, err := cmd.Flags().GetString(hostFlagName)
			if err != nil {
				return err
			}
			gcpCredentialFilePath, err := cmd.Flags().GetString(credFileFlagName)
			if err != nil {
				return err
			}

			pubsubClient, err := pkg.NewPubSubClient(cmd.Context(), projectID, emulatorHost, gcpCredentialFilePath)
			if err != nil {
				return errors.Wrap(err, "initialize pubsub client")
			}
			return subscribe(cmd.Context(), out, pubsubClient, topicIDs)
		},
	}
}

type subscriber struct {
	topic *pubsub.Topic
	sub   *pubsub.Subscription
}

// subscribe subscribes Pub/Sub messages
func subscribe(ctx context.Context, out io.Writer, pubsubClient *pkg.PubSubClient, topicIDs []string) error {
	eg := &errgroup.Group{}
	subscribers := make(chan *subscriber, len(topicIDs))
	for _, topicID := range topicIDs {
		topicID := topicID
		eg.Go(func() error {
			topic, err := pubsubClient.FindOrCreateTopic(ctx, topicID)
			if err != nil {
				return errors.Wrapf(err, "find or create topic %s", topicID)
			}

			fmt.Println(fmt.Sprintf("[start]creating unique subscription to %s...", topic.String()))
			sub, err := pubsubClient.CreateUniqueSubscription(ctx, topic)
			if err != nil {
				return errors.Wrapf(err, "create unique subscription to %s", topic.String())
			}
			subscribers <- &subscriber{topic: topic, sub: sub}
			_, _ = colorstring.Fprintf(out, "[green][success] created subscription to %s\n", topic.String())
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	close(subscribers)

	_, _ = fmt.Fprintln(out, "[start] waiting for publish...")
	for s := range subscribers {
		s := s
		eg.Go(func() error {
			err := s.sub.Receive(ctx, func(ctx context.Context, msg *pubsub.Message) {
				msg.Ack()
				_, _ = colorstring.Fprintf(out, "[green][success] got message published to %s, id: %s, data: %q\n", s.topic.ID(), msg.ID, string(msg.Data))
			})
			return errors.Wrapf(err, "receive message published to %s through %s subscription", s.topic.ID(), s.sub.ID())
		})
	}
	return eg.Wait()
}
