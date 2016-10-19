package game

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"

	"github.com/gothyra/toml"
	log "gopkg.in/inconshreveable/log15.v2"
)

type Config struct {
	host string
	port int
}

type Server struct {
	sync.RWMutex
	Players       map[string]Player
	onlineClients map[string]*Client
	Areas         map[string]Area
	Events        chan Event

	staticDir string
	Config    Config
}

func NewServer(staticDir string) *Server {
	return &Server{
		Players:       make(map[string]Player),
		onlineClients: make(map[string]*Client),
		Areas:         make(map[string]Area),
		staticDir:     staticDir,
		Events:        make(chan Event, 1000),
	}
}

func (s *Server) LoadConfig() error {
	log.Info("Loading config ...")

	configFileName := filepath.Join(s.staticDir, "/server.toml")
	fileContent, fileIoErr := ioutil.ReadFile(configFileName)
	if fileIoErr != nil {

		log.Info(fmt.Sprintf("%s could not be loaded: %v\n", configFileName, fileIoErr))
		return fileIoErr
	}

	config := Config{}
	if _, err := toml.Decode(string(fileContent), &config); err != nil {
		log.Info(fmt.Sprintf("%s could not be unmarshaled: %v\n", configFileName, err))
		return err
	}

	s.Config = config
	log.Info("Config loaded.")
	return nil
}

func (s *Server) LoadAreas() error {
	log.Info("Loading areas ...")
	areaWalker := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		fileContent, fileIoErr := ioutil.ReadFile(path)
		if fileIoErr != nil {
			log.Info(fmt.Sprintf("%s could not be loaded: %v", path, fileIoErr))
			return fileIoErr
		}

		area := Area{}
		if _, err := toml.Decode(string(fileContent), &area); err != nil {
			log.Info(fmt.Sprintf("%s could not be unmarshaled: %v", path, err))
			return err
		}

		log.Info(fmt.Sprintf("Loaded area %q", area.Name))
		// TODO: Lock
		s.Areas[area.Name] = area

		return nil
	}

	return filepath.Walk(s.staticDir+"/areas/", areaWalker)
}

func (s *Server) getPlayerFileName(playerName string) (bool, string) {
	if !IsValidUsername(playerName) {
		return false, ""
	}
	return true, s.staticDir + "/player/" + playerName + ".toml"
}

func IsValidUsername(playerName string) bool {
	r, err := regexp.Compile(`^[a-zA-Z0-9_-]{1,40}$`)
	if err != nil {
		return false
	}
	if !r.MatchString(playerName) {
		return false
	}
	return true
}

func (s *Server) LoadPlayer(playerName string) (bool, error) {
	ok, playerFileName := s.getPlayerFileName(playerName)
	if !ok {
		return false, nil
	}
	if _, err := os.Stat(playerFileName); err != nil {
		return false, nil
	}

	fileContent, fileIoErr := ioutil.ReadFile(playerFileName)
	if fileIoErr != nil {
		log.Info(fmt.Sprintf("%s could not be loaded: %v", playerFileName, fileIoErr))
		return true, fileIoErr
	}

	player := Player{}
	if _, err := toml.Decode(string(fileContent), &player); err != nil {
		log.Info(fmt.Sprintf("%s could not be unmarshaled: %v", playerFileName, err))
		return true, err
	}

	log.Info(fmt.Sprintf("Loaded player %q", player.Nickname))
	// TODO: Lock
	s.Players[player.Nickname] = player

	return true, nil
}

func (s *Server) GetPlayerByNick(nickname string) (Player, bool) {
	player, ok := s.Players[nickname]
	return player, ok
}

func (s *Server) CreatePlayer(nick string) {
	ok, playerFileName := s.getPlayerFileName(nick)
	if !ok {
		return
	}
	if _, err := os.Stat(playerFileName); err == nil {
		log.Info(fmt.Sprintf("Player %q does already exist.\n", nick))
		if _, err := s.LoadPlayer(nick); err != nil {
			log.Info(fmt.Sprintf("Player %q cannot be loaded: %v", nick, err))
		}
		return
	}
	player := Player{
		Nickname: nick,
		PC:       *NewPC(),
		Area:     "City",
		Room:     "Inn",
		Position: "1",
	}
	// TODO: Lock
	s.Players[player.Nickname] = player
}

func (s *Server) SavePlayer(player Player) bool {
	data := &bytes.Buffer{}
	encoder := toml.NewEncoder(data)
	err := encoder.Encode(player)
	if err == nil {
		ok, playerFileName := s.getPlayerFileName(player.Nickname)
		if !ok {
			return false
		}

		if ioerror := ioutil.WriteFile(playerFileName, data.Bytes(), 0644); ioerror != nil {
			log.Info(ioerror.Error())
			return true
		}
	} else {
		log.Info(err.Error())
	}
	return false
}

func (s *Server) OnExit(client Client) {
	s.SavePlayer(*client.Player)
	s.ClientLoggedOut(client.Player.Nickname)
}

func (s *Server) ClientLoggedIn(name string, client Client) {
	s.Lock()
	s.onlineClients[name] = &client
	s.Unlock()
}

func (s *Server) ClientLoggedOut(name string) {
	s.Lock()
	delete(s.onlineClients, name)
	s.Unlock()
}

func (s *Server) OnlineClients() []Client {
	s.RLock()
	defer s.RUnlock()

	online := []Client{}
	for _, online_clients := range s.onlineClients {
		online = append(online, *online_clients)
	}

	return online
}

func (s *Server) MapList() []string {
	s.RLock()
	defer s.RUnlock()

	maplist := []string{}
	// TODO: Remove Areas from Server
	for area := range s.Areas {
		maplist = append(maplist, area)
	}

	return maplist
}

// TODO: Remove from Server
func (s *Server) CreateRoom_as_cubes(area, room string) [][]Cube {

	biggestx := 0
	biggesty := 0
	biggest := 0

	roomCubes := []Cube{}
	// TODO: Remove Areas from Server
	for i := range s.Areas[area].Rooms {
		if s.Areas[area].Rooms[i].Name == room {
			roomCubes = s.Areas[area].Rooms[i].Cubes
			break
		}
	}

	for nick := range roomCubes {
		posx, _ := strconv.Atoi(roomCubes[nick].POSX)
		if posx > biggestx {
			biggestx = posx
		}

		posy, _ := strconv.Atoi(roomCubes[nick].POSY)
		if posy > biggesty {
			biggesty = posy
		}

	}

	if biggestx < biggesty {
		biggest = biggesty
	} else {
		biggest = biggestx
	}

	if biggest < 5 {
		biggest = biggest + 5
	}
	biggest++

	maparray := make([][]Cube, biggest)
	for i := range maparray {
		maparray[i] = make([]Cube, biggest)
	}

	for z := range roomCubes {
		posx, _ := strconv.Atoi(roomCubes[z].POSX)
		posy, _ := strconv.Atoi(roomCubes[z].POSY)
		if roomCubes[z].ID != "" {
			maparray[posx][posy] = roomCubes[z]
		}
	}

	return maparray
}

// TODO: Remove from Server
func (s *Server) HandleCommand(c Client, command string) {
	//TODO split command to get arguments

	event := Event{
		Client: &c,
	}

	switch command {

	case "l", "look", "map":
		event.Etype = "look"
	case "e", "east":
		event.Etype = "move_east"
	case "w", "west":
		event.Etype = "move_west"
	case "n", "north":
		event.Etype = "move_north"
	case "s", "south":
		event.Etype = "move_south"
	case "quit", "exit":
		event.Etype = "quit"
	default:
		event.Etype = "unknown"
	}
	s.Events <- event
}
