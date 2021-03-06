package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// connections stores all the hubs
var connections []*connectionPair

type connection struct {
	// Channel which triggers the connection to update the gameState
	doBroadcast chan bool
	// The connectionPair. Holds up to 2 connections.
	cp *connectionPair
	// playerNum represents the players Slot. Either 0 or 1
	playerNum int
}

// wsHandler implements the Handler Interface
type wsHandler struct{}

// incomingMessage is the representation of the ws message
type incomingMessage struct {
	Type string `json:"type"`
	Data string `json:"data"`
	ID   int    `json:"id"`
}

// reader reads the moves from the clients ws-connection
func (c *connection) reader(wg *sync.WaitGroup, wsConn *websocket.Conn) {
	defer wg.Done()
	for {
		//Reading next move from connection here
		_, clientMoveMessage, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		// THIS PART TAKES IN THE MESSAGES AND PARSES IT TO GET FIELDS
		field := string(clientMoveMessage[:]) //Getting FieldValue From Player Action
		message := &incomingMessage{}
		if err := json.Unmarshal([]byte(field), &message); err != nil {
			panic(err)
		}
		c.cp.gs.recordMove(message)
		c.cp.receiveMove <- true //telling connectionPair to broadcast the gameState
	}
}

// writer broadcasts the current gameState to the two players in a connectionPair
func (c *connection) writer(wg *sync.WaitGroup, wsConn *websocket.Conn) {
	defer wg.Done()
	for range c.doBroadcast {
		sendGameStateToConnection(wsConn, c)
	}
}

// getConnectionPairWithEmptySlot looks trough all connectionPairs and finds one which has only 1 player
// if there is none a new connectionPair is created and the player is added to that pair
func getConnectionPairWithEmptySlot() (*connectionPair, int) {
	sizeBefore := len(connections)
	// find connections with 1 player first and pair if possible
	for _, h := range connections {
		if len(h.connections) == 1 {
			log.Printf("Players paired")
			return h, len(h.connections)
		}
	}

	//TODO: I need to remove orphaned connectionPairs from the stack

	// if no emtpy slow was found at all, we create a new connectionPair
	h := newConnectionPair()
	connections = append(connections, h)
	log.Printf("Player seated in new connectionPair no. %v", len(connections))
	return connections[sizeBefore], 0
}

// ServeHTTP is the routers HandleFunc for websocket connections
// connections are upgraded to websocket connections and the player is added
// to a connection pair
func (wsh wsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	// upgrader is needed to upgrade the HTTP Connection to a websocket Connection
	upgrader := &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	//Upgrading HTTP Connection to websocket connection
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("error upgrading %s", err)
		return
	}

	//Adding Connection to connectionPair
	cp, pn := getConnectionPairWithEmptySlot()
	c := &connection{doBroadcast: make(chan bool), cp: cp, playerNum: pn}
	c.cp.addConnection(c)

	//If the connectionPair existed before but one player was disconnected
	//we can now reinitialize the gameState after the remaining player has
	//been paired again
	if c.cp.gs.StatusMessage == resetWaitPaired {
		c.cp.gs = newGameState()
		//there is already one player connected when we re-pair
		c.cp.gs.numberOfPlayers = 1
		log.Println("gamestate resetted")
	}

	//inform the gameState about the new player
	c.cp.gs.addPlayer()
	//telling connectionPair to broadcast the gameState
	c.cp.receiveMove <- true

	//creating the writer and reader goroutines
	//the websocket connection is open as long as these goroutines are running
	var wg sync.WaitGroup
	wg.Add(2)
	go c.writer(&wg, wsConn)
	go c.reader(&wg, wsConn)
	wg.Wait()
	wsConn.Close()
}

// sendGameStateToConnection broadcasts the current gameState as JSON to all players
// within a connectionPair
func sendGameStateToConnection(wsConn *websocket.Conn, c *connection) {
	err := wsConn.WriteMessage(websocket.TextMessage, c.cp.gs.gameStateToJSON())
	//removing connection if updating gameState fails
	if err != nil {
		c.cp.removeConnection(c)
	}
}
