package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strconv"
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
		fmt.Printf("Error occurred while connecting to server %s\n", err)
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
		fmt.Printf("Error occurred while starting the bi-directional stream with the server %s\n", err)
		return err
	}
	return nil
}

func login() error {

	loginEvent := pkg.ChatEvent{
		Command: &pkg.ChatEvent_Login{
			Login: me,
		},
	}
	sendErr := stream.Send(&loginEvent)
	if sendErr != nil {
		fmt.Printf("Failed to send message to server: %v\n", sendErr)
		return sendErr
	}

	// Receive Login Response
	_, err := stream.Recv()
	if err != nil {
		fmt.Printf("Failed to login to server: %v\n", err)
		return err
	}

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
		fmt.Printf("Problem signing in: %v\n", err)
		authenticate()
		return
	}
	me = &pkg.Client{
		ClientId: response.GetId(),
		Name:     response.GetFirstName(),
	}

	fmt.Printf("Hello %s, your ClientId is %s\n", me.Name, me.ClientId)
}

func signup() {

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("Enter Email: ")
	scanner.Scan()
	email := strings.TrimSpace(scanner.Text())

	fmt.Printf("Enter Password: ")
	scanner.Scan()
	password := strings.TrimSpace(scanner.Text())

	fmt.Printf("Enter First Name: ")
	scanner.Scan()
	first := strings.TrimSpace(scanner.Text())

	fmt.Printf("Enter Last Name: ")
	scanner.Scan()
	last := strings.TrimSpace(scanner.Text())

	fmt.Printf("Enter Phone Number: ")
	scanner.Scan()
	phone := strings.TrimSpace(scanner.Text())

	response, err := authClient.SignUp(
		context.TODO(),
		&pkg.SignUpRequest{
			Email:       email,
			Password:    password,
			FirstName:   first,
			LastName:    last,
			PhoneNumber: phone,
		},
	)

	if err != nil {
		fmt.Printf("Unable to signup new user. Error: %v\n", err)
		signup()
		return
	}

	fmt.Printf("New user created with Id %s\n", response.GetId())
	mainMenu()
}

func searchAccounts(query string) ([]*pkg.Account, error) {
	searchResponse, err := authClient.SearchAccounts(
		context.TODO(),
		&pkg.SearchAccountsRequest{
			SearchQuery: query,
			Page:        0,
			Size:        5,
		},
	)
	if err != nil {
		fmt.Printf("Error searching accounts with %s. Error: %v\n", query, err)
		return nil, err
	}
	return searchResponse.GetMembers(), nil
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
		fmt.Printf("Unable to setup the connection. Exiting\n")
		os.Exit(1)
	}
	defer connection.Close()

	mainMenu()

	// 3. Start Stream
	startStream()

	// 4. Login
	if err := login(); err != nil {
		fmt.Printf("Error while logging in to register a chat stream. %v\n", err)
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
				fmt.Printf("Failed to receive a note : %v\n", err)
				close(waitc)
				return
			}

			if login := in.GetLogin(); login != nil {
				fmt.Println(login.GetName(), "logged in")
			} else if message := in.GetMessage(); message != nil {
				fmt.Printf("\nFrom %s: %s\n", message.GetFrom().GetName(), message.GetContent())
			}
		}
	}()

	// Reading message from stdin and send to server
	go func() {
		var conversation pkg.Conversation
		var recipientName string

		regex, _ := regexp.Compile("^/search ")
		messageScanner := bufio.NewScanner(os.Stdin)

		for messageScanner.Scan() {
			text := strings.TrimSpace(messageScanner.Text())
			if text == "" {
				continue
			}

			matched := regex.MatchString(text)

			if matched {
				textSplit := strings.Split(text, " ")
				query := textSplit[1]

				accounts, searchError := searchAccounts(query)
				if searchError != nil {
					panic(searchError)
				}

				fmt.Printf("Select the user or 0 for more results\n")
				for index, acc := range accounts {
					fmt.Printf("%d - %s %s\n", index+1, acc.FirstName, acc.LastName)
				}
				messageScanner.Scan()
				personIndexStr := strings.TrimSpace(messageScanner.Text())
				personIndex, _ := strconv.Atoi(personIndexStr)
				selectedAccount := accounts[personIndex-1]
				recipientName = selectedAccount.FirstName

				conversationResponse, err := chatClient.CreateConversation(
					ctx,
					&pkg.ConversationRequest{
						Members: []*pkg.Client{
							me,
							&pkg.Client{
								ClientId: selectedAccount.Id,
								Name:     selectedAccount.FirstName,
							},
						},
					})

				if err != nil {
					fmt.Printf("Unable to create conversation, error returned from server: %v\n", err)
					continue
				}

				fmt.Printf("To %s: ", selectedAccount.FirstName)

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
				fmt.Printf("Failed to send message to server: %v\n", err)
			}

			if conversation.Id != "" {
				fmt.Printf("To %s: ", recipientName)
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
	fmt.Println("chatClient exited")
}
