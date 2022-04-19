package main

import (
	"bufio"
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/Madslick/chit-chat-go/pkg"
	"google.golang.org/grpc"
)

var ctx context.Context
var connection *grpc.ClientConn
var client pkg.ChatroomClient
var stream pkg.Chatroom_ConverseClient

var chatClient *pkg.Client

func connect(connectionString string) error {
	//ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	//defer cancel()

	var err error
	connection, err = grpc.DialContext(ctx, connectionString, grpc.WithInsecure())
	if err != nil {
		log.Fatal("Error occurred while connecting to server", err)
		return err
	}

	client = pkg.NewChatroomClient(connection)
	stream, err = client.Converse(context.Background())
	if err != nil {
		log.Fatal("Error occurred while starting the bi-directional stream with the server", err)
		return err
	}

	return nil
}

func login() error {
	log.Println("Who are you?")
	loginScanner := bufio.NewScanner(os.Stdin)
	loginScanner.Scan()
	name := strings.TrimSpace(loginScanner.Text())

	// Send Login over to server
	chatClient = &pkg.Client{Name: name}
	loginEvent := pkg.ChatEvent{
		Command: &pkg.ChatEvent_Login{
			Login: chatClient,
		},
	}
	sendErr := stream.Send(&loginEvent)
	if sendErr != nil {
		log.Fatalf("Failed to send message to server: %v", sendErr)
		return sendErr
	}

	// Receive Login Response
	loginResponse, err := stream.Recv()
	if err != nil {
		log.Fatalf("Failed to login to server")
		return err
	}

	if login := loginResponse.GetLogin(); login != nil {
		chatClient.ClientId = login.GetClientId()
	}
	log.Println("Type '/w <name>' to send someone a message")

	return nil
}

func main() {
	ctx = context.TODO()

	// 1. Pull Command Line arguments
	var serverConnection string
	flag.StringVar(&serverConnection, "s", "chit-chat-go:3000", "The host:port to connect to the server")

	flag.Parse()

	// 2. Start Connection
	// 3. Start Stream
	err := connect(serverConnection)
	if err != nil {
		log.Fatal("Unable to setup the connection & stream. Exiting")
		os.Exit(1)
	}
	defer connection.Close()
	defer stream.CloseSend()

	// 4. Login
	if err := login(); err != nil {
		log.Fatal("Error while logging in to register a chat stream", err)
		os.Exit(1)
	}

	// 5. Start Goroutines for input/output
	waitc := make(chan struct{})

	// log.Println("Who do you wanna call?")
	// mainScanner.Scan()
	// memberName := strings.TrimSpace(mainScanner.Text())

	// conversationResponse, err := client.CreateConversation(
	// 	ctx,
	// 	&pkg.ConversationRequest{
	// 		Members: []*pkg.Client{chatClient, &pkg.Client{Name: memberName}},
	// 	})

	// if err != nil {
	// 	log.Fatal("Unable to create conversation, error returned from server: ", err)
	// }

	// conversation := pkg.Conversation{
	// 	Id:      conversationResponse.GetId(),
	// 	Members: conversationResponse.GetMembers(),
	// }

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
		var conversation pkg.Conversation

		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text == "" {
				continue
			}

			matched, err := regexp.MatchString("^/w ", text)
			if err != nil {
				log.Fatal("Something wrong with the regex dude")
				continue
			}

			if matched {
				textSplit := strings.Split(text, " ")
				targetName := textSplit[1]

				conversationResponse, err := client.CreateConversation(
					ctx,
					&pkg.ConversationRequest{
						Members: []*pkg.Client{chatClient, &pkg.Client{Name: targetName}},
					})

				if err != nil {
					log.Fatal("Unable to create conversation, error returned from server: ", err)
					continue
				}

				log.Println("Now chatting:", targetName)

				conversation = pkg.Conversation{
					Id:      conversationResponse.GetId(),
					Members: conversationResponse.GetMembers(),
				}
				continue
			}

			// Send a message on the existing Conversation
			message := pkg.Message{}
			message.Conversation = &conversation
			message.From = chatClient
			message.Content = text

			err = stream.Send(&pkg.ChatEvent{
				Command: &pkg.ChatEvent_Message{Message: &message},
			})
			if err != nil {
				log.Fatalf("Failed to send message to server: %v", err)
			}
		}
	}()

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
