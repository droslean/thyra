package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/gothyra/thyra/game"
)

func main() {
	staticDir := os.Getenv("THYRA_STATIC")
	if len(staticDir) == 0 {
		pwd, _ := os.Getwd()
		staticDir = filepath.Join(pwd, "static")
		log.Println("Set THYRA_STATIC if you wish to configure the directory for static content")
	}
	log.Printf("Using %s for static content\n", staticDir)

	server := game.NewServer(staticDir)

	if err := server.LoadConfig(); err != nil {
		os.Exit(1)
	}

	if err := server.LoadLevels(); err != nil {
		os.Exit(1)
	}

	ln, err := net.Listen("tcp", server.Config.Interface)
	if err != nil {
		log.Println(err.Error())
		os.Exit(1)
	}

	log.Printf("Listen on: %s", ln.Addr())

	msgchan := make(chan string)
	addchan := make(chan game.Client)
	rmchan := make(chan game.Client)

	go handleMessages(msgchan, addchan, rmchan)

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println(err)
			continue
		}

		go handleConnection(conn, msgchan, addchan, rmchan, server)
	}
}

func promptMessage(c net.Conn, bufc *bufio.Reader, message string) string {
	for {
		io.WriteString(c, message)
		answer, _, _ := bufc.ReadLine()
		if string(answer) != "" {
			return string(answer)
		}
	}
}

func handleConnection(c net.Conn, msgchan chan<- string, addchan chan<- game.Client, rmchan chan<- game.Client, server *game.Server) {
	bufc := bufio.NewReader(c)
	defer c.Close()

	log.Println("New connection open:", c.RemoteAddr())
	io.WriteString(c, server.Config.Motd)

	var nickname string
	questions := 0
	for {
		if questions >= 3 {
			io.WriteString(c, "See you\n\r")
			return
		}

		nickname = promptMessage(c, bufc, "Whats your Nick?\n\r  ")
		isValidName := server.IsValidUsername(nickname)
		if !isValidName {
			questions++
			io.WriteString(c, fmt.Sprintf("Username %s is not valid (0-9a-z_-).\n\r", nickname))
			continue
		}

		exists, err := server.LoadPlayer(nickname)
		if err != nil {
			io.WriteString(c, fmt.Sprintf("%s\n\r", err.Error()))
			return
		}
		if exists {
			break
		}

		questions++
		io.WriteString(c, fmt.Sprintf("Username %s does not exists.\n\r", nickname))
		answer := promptMessage(c, bufc, "Do you want to create that user? [y|n] ")

		if answer == "y" || answer == "yes" {
			server.CreatePlayer(nickname)
			break
		}
	}

	player, playerLoaded := server.GetPlayerByNick(nickname)

	if !playerLoaded {
		log.Println("problem getting user object")
		io.WriteString(c, "Problem getting user object\n")
		return
	}

	client := game.NewClient(c, player)

	if strings.TrimSpace(client.Nickname) == "" {
		log.Println("invalid username")
		io.WriteString(c, "Invalid Username\n")
		return
	}

	// Register user
	addchan <- client
	defer func() {
		msgchan <- fmt.Sprintf("User %s left the chat room.\n\r", client.Nickname)
		log.Printf("Connection from %v closed.\n", c.RemoteAddr())
		rmchan <- client
	}()
	io.WriteString(c, fmt.Sprintf("Welcome, %s!\n\n\r", client.Player.Nickname))
	server.PlayerLoggedIn(client.Nickname)

	// I/O
	go client.ReadLinesInto(msgchan, server)
	client.WriteLinesFrom(client.Ch)
}

func handleMessages(msgchan <-chan string, addchan <-chan game.Client, rmchan <-chan game.Client) {
	clients := make(map[net.Conn]chan<- string)

	for {
		select {
		case msg := <-msgchan:
			log.Printf("New message: %s", msg)
			for _, ch := range clients {
				go func(mch chan<- string) { mch <- "\033[1;33;40m" + msg + "\033[m\n\r" }(ch)
			}
		case client := <-addchan:
			log.Printf("New client: %v\n\r\n\r", client.Conn)
			clients[client.Conn] = client.Ch
		case client := <-rmchan:
			log.Printf("Client disconnects: %v\n\r\n\r", client.Conn)
			delete(clients, client.Conn)
		}
	}
}
