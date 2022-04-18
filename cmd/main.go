package main

import (
	"bufio"
	"flag"
	"context"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Madslick/chit-chat-go/pkg"
	"google.golang.org/grpc"
)

func main() {
	var serverConnection string
	//ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ctx := context.TODO()
	//defer cancel()

	flag.StringVar(&serverConnection, "s", "chit-chat-go:3000", "The host:port to connect to the server")
	cc, err := grpc.DialContext(ctx, serverConnection, grpc.WithInsecure())
	if err != nil {
		log.Fatal("Error occurred while connecting to server", err)
		return
	}
	defer cc.Close()

	client := pkg.NewChatroomClient(cc)
	stream, err := client.Converse(context.Background())
	if err != nil {
		log.Fatal("Error occurred while starting the bi-directional stream with the server", err)
		return
	}
	defer stream.CloseSend()

	waitc := make(chan struct{})

	// Take Login Input
	log.Println("Who are you?")
	mainScanner := bufio.NewScanner(os.Stdin)
	mainScanner.Scan()
	name := strings.TrimSpace(mainScanner.Text())

	// Send Login over to server
	chatClient := &pkg.Client{Name: name}
	loginEvent := pkg.ChatEvent{
		Command: &pkg.ChatEvent_Login{
			Login: chatClient,
		},
	}
	sendErr := stream.Send(&loginEvent)
	if sendErr != nil {
		log.Fatalf("Failed to send message to server: %v", err)
	}

	// Receive Login Response
	loginResponse, _ := stream.Recv()
	if err != nil {
		log.Fatalf("Failed to login to server")
	}

	if login := loginResponse.GetLogin(); login != nil {
		chatClient.ClientId = login.GetClientId()
	}

	log.Println("Who do you wanna call?")
	mainScanner.Scan()
	memberName := strings.TrimSpace(mainScanner.Text())

	conversationResponse, err := client.CreateConversation(
		ctx,
		&pkg.ConversationRequest{
			Members: []*pkg.Client{chatClient, &pkg.Client{Name: memberName}},
		})

	if err != nil {
		log.Fatal("Unable to create conversation, error returned from server: ", err)
	}

	conversation := pkg.Conversation{
		Id:      conversationResponse.GetId(),
		Members: conversationResponse.GetMembers(),
	}

	// Receiving message from server
	go func() {
		for {
			in, err := stream.Recv()
			if err == io.EOF {
				close(waitc)
				return
			}
			if err != nil {
				log.Fatalf("Failed to receive a note : %v", err)
			}

			if login := in.GetLogin(); login != nil {
				log.Println(login.GetName(), "logged in")
			} else if message := in.GetMessage(); message != nil {
				log.Println(message.GetFrom().GetName(), ":", message.GetContent())
			}
		}
	}()

	// Reading message from stdin and send to server
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}
			// event := nil
			// if event == nil {
			// 	continue
			// }
			message := pkg.Message{}
			message.Conversation = &conversation
			message.From = chatClient
			message.Content = text

			err := stream.Send(&pkg.ChatEvent{
				Command: &pkg.ChatEvent_Message{Message: &message},
			})
			if err != nil {
				log.Fatalf("Failed to send message to server: %v", err)
			}
		}
	}()
	log.Println("client started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

loop:
	for {
		select {
		case <-waitc:
			break loop
		case <-quit:
			break loop
		}
	}
	log.Println("client exited")
}