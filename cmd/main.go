package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/abiosoft/ishell/v2"
	"google.golang.org/grpc"

	"github.com/Madslick/chit-chat-go-client/pkg"
)

var ctx context.Context
var connection *grpc.ClientConn
var chatClient pkg.ChatroomClient
var authClient pkg.AuthClient
var stream pkg.Chatroom_ConverseClient

var me *pkg.Client
var selectedAccount *pkg.Account
var conversation pkg.Conversation

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

func authenticate(email string, password string) {

	response, err := authClient.SignIn(
		context.TODO(),
		&pkg.SignInRequest{
			Email:    email,
			Password: password,
		},
	)
	if err != nil {
		fmt.Printf("Problem signing in: %v\n", err)
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

func receive(c *ishell.Context, ch chan struct{}, acc *pkg.Account) {
	for {
		in, err := stream.Recv()
		if err == io.EOF || err != nil {
			c.Printf("Error receiving data from stream: %s\n", err)
			ch <- struct{}{}
			return
		}

		if login := in.GetLogin(); login != nil {
			c.Println(login.GetName(), "logged in")
		} else if message := in.GetMessage(); message != nil {
			c.Printf("\nFrom %s: %s\n", message.GetFrom().GetName(), message.GetContent())
			if acc != nil {
				c.Printf("To %s: ", acc.FirstName)
			} else {
				c.Print(">>> ")
			}
		}
	}
}

func transmit(c *ishell.Context, ch chan struct{}, acc *pkg.Account) {
	messageScanner := bufio.NewScanner(os.Stdin)
	c.Printf("To %s: ", acc.FirstName)
	for messageScanner.Scan() {
		msg := strings.TrimSpace(messageScanner.Text())
		if msg == "" {
			continue
		}

		if msg == "/break" {
			ch <- struct{}{}
			c.Printf("Exited Chat.\n")
			return
		}

		message := pkg.Message{}
		message.Conversation = &conversation
		message.From = me
		message.Content = msg

		err := stream.Send(&pkg.ChatEvent{
			Command: &pkg.ChatEvent_Message{Message: &message},
		})
		if err != nil {
			fmt.Printf("Failed to send message to server: %v\n", err)
		}

		c.Printf("To %s: ", acc.FirstName)

	}

}

func main() {
	// Main Function for chit-chat-go
	ctx = context.TODO()

	// 1. Pull Command Line arguments
	var serverConnection string
	flag.StringVar(&serverConnection, "s", "chit-chat-go:3000", "The host:port to connect to the server")
	flag.Parse()

	shell := ishell.New()
	shell.Println("Welcome to Chit-Chat-Go. Type help for the available commands")
	shell.SetMultiChoicePrompt(" >>", " - ")

	connect(serverConnection)
	breakChan := make(chan struct{})

	shell.AddCmd(&ishell.Cmd{
		Name: "login",
		Func: func(c *ishell.Context) {
			c.ShowPrompt(false)
			defer c.ShowPrompt(true)

			// prompt for input
			c.Print("Email: ")
			email := c.ReadLine()
			c.Print("Password: ")
			password := c.ReadPassword()

			authenticate(email, password)

			startStream()

			login()

			go receive(c, breakChan, selectedAccount)
		},
		Help: "simulate a login",
	})

	shell.AddCmd(&ishell.Cmd{
		Name: "search",
		Help: "Search for a user to start a conversation with",
		Func: func(c *ishell.Context) {
			defer fmt.Println("Search CMD ended.")
			c.ShowPrompt(false)
			defer c.ShowPrompt(true)

			if me.GetClientId() == "" {
				c.Println("You must login first")
				return
			}
			c.Print("Enter a name to search: ")
			query := c.ReadLine()
			accounts, err := searchAccounts(query)
			if err != nil {
				c.Err(err)
			}

			account_names := []string{}
			for _, account := range accounts {
				account_names = append(account_names, fmt.Sprintf("%s %s", account.GetFirstName(), account.GetLastName()))
			}

			choice := c.MultiChoice(account_names, "One of these people ?")
			selectedAccount = accounts[choice]

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
			for _, msg := range conversationResponse.Messages {
				fmt.Printf("From %s: %s\n", msg.GetFrom().GetName(), msg.GetContent())
			}

			if err != nil {
				fmt.Printf("Unable to create conversation, error returned from server: %v\n", err)
				return
			}

			conversation = pkg.Conversation{
				Id:      conversationResponse.GetId(),
				Members: conversationResponse.GetMembers(),
			}

			go transmit(c, breakChan, selectedAccount)

			<-breakChan

			selectedAccount = nil

		},
	})

	shell.Run()
}
