package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/Madslick/chit-chat-go-client/pkg"
	"google.golang.org/grpc"
)

var ctx context.Context
var connection *grpc.ClientConn
var chatClient pkg.ChatroomClient
var authClient pkg.AuthClient
var stream pkg.Chatroom_ConverseClient

var me *pkg.Client

func connect(connectionString string) error {
	//ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	//defer cancel()

	var err error
	connection, err = grpc.DialContext(ctx, connectionString, grpc.WithInsecure())
	if err != nil {
		log.Fatal("Error occurred while connecting to server", err)
		return err
	}

	chatClient = pkg.NewChatroomClient(connection)
	authClient = pkg.NewAuthClient(connection)

	return nil
}

func startStream() error {
	var err error
	stream, err = chatClient.Converse(context.Background())
	if err != nil {
		log.Fatal("Error occurred while starting the bi-directional stream with the server", err)
		return err
	}
	log.Println("Stream Started")
	return nil
}

func login() error {

	log.Printf("Registering Stream with %s %s\n", me.GetName(), me.GetClientId())
	loginEvent := pkg.ChatEvent{
		Command: &pkg.ChatEvent_Login{
			Login: me,
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
		log.Fatalf("Failed to login to server: %s", err)
		return err
	}

	if login := loginResponse.GetLogin(); login != nil {
		me.ClientId = login.GetClientId()
	}
	fmt.Println("Type '/w <name>' to send someone a message")

	return nil
}

func mainMenu() {
	fmt.Println("Type 1 to login or 2 to signup")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	input := strings.TrimSpace(scanner.Text())
	if input == "1" {
		authenticate()
	} else if input == "2" {
		signup()
	}
}

func authenticate() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("Enter Email: ")
	scanner.Scan()
	email := strings.TrimSpace(scanner.Text())

	fmt.Printf("Enter Password: ")
	scanner.Scan()
	password := strings.TrimSpace(scanner.Text())

	response, err := authClient.SignIn(
		context.TODO(),
		&pkg.SignInRequest{
			Email:    email,
			Password: password,
		},
	)
	if err != nil {
		log.Fatalf("Problem signing in: %s", err)
		authenticate()
	}
	me = &pkg.Client{
		ClientId: response.GetId(),
		Name:     response.GetFirstName(),
	}

	log.Printf("Hello %s, your ClientId is %s\n", me.Name, me.ClientId)
}

func signup() {

}

func main() {
	ctx = context.TODO()

	// 1. Pull Command Line arguments
	var serverConnection string
	flag.StringVar(&serverConnection, "s", "chit-chat-go:3000", "The host:port to connect to the server")
	flag.Parse()

	// 2. Start Connection

	err := connect(serverConnection)
	if err != nil {
		log.Fatal("Unable to setup the connection. Exiting")
		os.Exit(1)
	}
	defer connection.Close()

	mainMenu()

	// 3. Start Stream
	startStream()

	// 4. Login
	if err := login(); err != nil {
		log.Fatal("Error while logging in to register a chat stream", err)
		os.Exit(1)
	}
	defer stream.CloseSend()

	// 5. Start Goroutines for input/output
	waitc := make(chan struct{})

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

		messageScanner := bufio.NewScanner(os.Stdin)
		for messageScanner.Scan() {
			text := strings.TrimSpace(messageScanner.Text())
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

				conversationResponse, err := chatClient.CreateConversation(
					ctx,
					&pkg.ConversationRequest{
						Members: []*pkg.Client{me, &pkg.Client{Name: targetName}},
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
			message.From = me
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
	log.Println("chatClient exited")
}
