package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	f "net/url"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/fatih/color"
	_ "github.com/go-sql-driver/mysql"
)

var (
	config               = new(Configuration)
	countGuild       int = 0
	ServersGuilds        = make(map[int]string)
	activeCommands       = make(map[string]command)
	disabledCommands     = make(map[string]command)
	CooldownCMD          = make(map[string]int64)
	footer               = new(discordgo.MessageEmbedFooter)
	mem              runtime.MemStats
)

type Configuration struct {
	Game    string `json:"game"`
	Prefix  string `json:"prefix"`
	Token   string `json:"token"`
	OwnerID string `json:"owner_id"`
	MaxProc int    `json:"maxproc"`
}

type command struct {
	Name string
	Help string

	OwnerOnly     bool
	RequiresPerms bool

	PermsRequired int64

	Exec func(*discordgo.Session, *discordgo.MessageCreate, []string)
}

func main() {
	loadConfig()
	runtime.GOMAXPROCS(config.MaxProc)
	session, err := discordgo.New(fmt.Sprintf("Bot %s", string(config.Token)))
	if err != nil {
		errorln("Failed create Discord session err: %s", err)
		return
	}
	session.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildMembers | discordgo.IntentsAllWithoutPrivileged)
	_, err = session.User("@me")
	if err != nil {
		errorln("Session @me err: %s", err)
	}
	// E V E N T S
	session.AddHandler(guildJoin)
	session.AddHandler(guildLeave)
	session.AddHandler(messageCreate)

	err = session.Open()
	if err != nil {
		errorln("Failed opening connection err: %s", err)
		return
	}

	color.Green("Bot is now running. Press CTRL-C to exit.")
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM, os.Interrupt, os.Kill)
	<-sc
	session.Close()
}

// I N I T

func init() {
	footer.Text = "Last Update: 3/24/2022\nLast Bot reboot: " + time.Now().Format("2006-01-02 3:4:5 pm")
	newCommand("help", 0, false, cmdHelp).add()
	newCommand("info", 0, false, cmdInfo).add()
	newCommand("deck", 0, false, cmdDeck).setHelp("deck player trophies\nExample: deck SomePlayer 6400\n*Note: Currently minimum trophies search is 6000. Also you can use without trophies this command but maybe it will be not 100%% correct in finding selected player.").add()
	newCommand("stats", 0, false, cmdStats).setHelp("deck player\nExample: deck SomePlayer").add()
}

// E V E N T S
func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}

	repeatblock := strings.Count(m.Content, config.Prefix)
	if repeatblock > 1 {
		return
	}
	//End fix

	guildDetails, err := guildDetails(m.ChannelID, "", s)
	if err != nil {
		return
	}

	prefix, err := activePrefix(m.ChannelID, s)
	if err != nil {
		return
	}

	if !strings.HasPrefix(m.Content, config.Prefix) && !strings.HasPrefix(m.Content, prefix) {
		return
	}
	parseCommand(s, m, guildDetails, func() string {
		if strings.HasPrefix(m.Content, config.Prefix) {
			return strings.TrimPrefix(m.Content, config.Prefix)
		}
		return strings.TrimPrefix(m.Content, prefix)
	}())
}

func guildJoin(s *discordgo.Session, m *discordgo.GuildCreate) {
	if m.Unavailable {
		errorln("Unavailable to join guild %s", m.Guild.ID)
		return
	}
	color.Yellow("Joined to server id: %s name: %s", m.Guild.ID, m.Guild.Name)
	countGuild++
	if err := s.UpdateGameStatus(0, fmt.Sprintf("Clash Royale | !help | Servers: %d", countGuild)); err != nil {
		color.Red("Can't set bot game status, error: %s", err)
		return
	}
}

func guildLeave(s *discordgo.Session, m *discordgo.GuildDelete) {
	if m.Unavailable {
		guild, err := guildDetails("", m.Guild.ID, s)
		if err != nil {
			errorln("Unavailable guild id: %s", m.Guild.ID)
			return
		}
		errorln("Unavailable guild id: %s name: %s", m.Guild.ID, guild.Name)
		return
	}
	countGuild--
	if err := s.UpdateGameStatus(0, fmt.Sprintf("Clash Royale | !help | Servers: %d", countGuild)); err != nil {
		color.Red("Can't set bot game status, error: %s", err)
		return
	}
	color.Yellow("Leaved guild id: %s name: %s", m.Guild.ID, m.Name)
}

// F U N C T I O N S

func errorln(format string, a ...interface{}) {
	str := "Error: "
	str += format
	color.Red(str, a...)
	file, err := os.OpenFile("errors.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(file)
	log.Printf(str, a...)
	defer file.Close()
}

func guildDetails(channelID, guildID string, s *discordgo.Session) (guildDetails *discordgo.Guild, err error) {
	if guildID == "" {
		var channel *discordgo.Channel
		channel, err = channelDetails(channelID, s)
		if err != nil {
			return
		}
		guildID = channel.GuildID
	}

	guildDetails, err = s.State.Guild(guildID)
	if err != nil {
		if err == discordgo.ErrStateNotFound {
			guildDetails, err = s.Guild(guildID)
			if err != nil {
				errorln("Getting guild details =>", guildID, err)
			}
		}
	}
	return
}

func channelDetails(channelID string, s *discordgo.Session) (channelDetails *discordgo.Channel, err error) {
	channelDetails, err = s.State.Channel(channelID)
	if err != nil {
		if err == discordgo.ErrStateNotFound {
			channelDetails, err = s.Channel(channelID)
			if err != nil {
				errorln("Getting channel details =>", channelID, err)
			}
		}
	}
	return
}

func permissionDetails(authorID, channelID string, s *discordgo.Session) (userPerms int64, err error) {
	userPerms, err = s.State.UserChannelPermissions(authorID, channelID)
	if err != nil {
		if err == discordgo.ErrStateNotFound {
			userPerms, err = s.UserChannelPermissions(authorID, channelID)
			if err != nil {
				errorln("Getting permission details err: %s", err)
			}
		}
	}
	return
}

// Config

func loadJSON(path string, v interface{}) error {
	f, err := os.OpenFile(path, os.O_RDONLY, 0600)
	if err != nil {
		errorln("Loading Err > Path: %s Err: %s", path, err)
		return err
	}

	if err := json.NewDecoder(f).Decode(v); err != nil {
		errorln("Loading Err > Path: %s Err: %s", path, err)
		return err
	}
	return nil
}
func saveJSON(path string, data interface{}) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		errorln("Saving Err > Path: %s Err: %s", path, err)
		return err
	}

	if err = json.NewEncoder(f).Encode(data); err != nil {
		errorln("Saving Err > Path: %s Err: %s", path, err)
		return err
	}
	return nil
}

func loadConfig() error {
	return loadJSON("config.json", config)
}

func saveConfig() error {
	return saveJSON("config.json", config)
}

//Commands

func newCommand(name string, permissions int64, needsPerms bool, f func(*discordgo.Session, *discordgo.MessageCreate, []string)) command {
	return command{
		Name:          name,
		PermsRequired: permissions,
		RequiresPerms: needsPerms,
		Exec:          f,
	}
}
func (c command) alias(a string) command {
	activeCommands[strings.ToLower(a)] = c
	return c
}

func (c command) setHelp(help string) command {
	c.Help = help
	return c
}

func (c command) ownerOnly() command {
	c.OwnerOnly = true
	return c
}
func parseCommand(s *discordgo.Session, m *discordgo.MessageCreate, guildDetails *discordgo.Guild, message string) {
	msglist := strings.Fields(message)
	if len(msglist) == 0 {
		return
	}
	t := time.Now().Unix()
	if t < CooldownCMD[m.Author.ID] {
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xad2e2e,
			Description: fmt.Sprintf("You calling commands so fast, please slow down!"),
			Footer:      footer,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Required wait time:", Value: "3 seconds", Inline: false},
			},
		})
		return
	} else {
		CooldownCMD[m.Author.ID] = t + 3 //+3 sec wait
	}
	isOwner := m.Author.ID == config.OwnerID
	commandName := strings.ToLower(func() string {
		if strings.HasPrefix(message, " ") {
			return " " + msglist[0]
		}
		return msglist[0]
	}())

	color.Blue(fmt.Sprintf("(debug): Author ID: %s, Author Username: %s#%s, Guild ID: %s, Server: %s, Command: %s", m.Author.ID, m.Author.Username, m.Author.Discriminator, guildDetails.ID, guildDetails.Name, m.Content))

	if command, ok := activeCommands[commandName]; ok && commandName == strings.ToLower(command.Name) {
		userPerms, err := permissionDetails(m.Author.ID, m.ChannelID, s)
		if err != nil {
			s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
				Color:       0xad2e2e,
				Description: fmt.Sprintf("Can't parse permissions!"),
				Footer:      footer,
			})
			return
		}

		hasPerms := userPerms&command.PermsRequired > 0
		if (!command.OwnerOnly && !command.RequiresPerms) || (command.RequiresPerms && hasPerms) || isOwner {
			command.Exec(s, m, msglist)
			return
		}
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xad2e2e,
			Description: fmt.Sprintf("You don't have permissions for this command!"),
			Footer:      footer,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Required roles:", Value: "👑Ownership", Inline: false},
			},
		})
		return
	} else {
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xad2e2e,
			Description: fmt.Sprintf("Unknown command."),
			Footer:      footer,
			Fields: []*discordgo.MessageEmbedField{
				{Name: "Check commands:", Value: config.Prefix + "help", Inline: false},
			},
		})
		return
	}
	activeCommands["bigmoji"].Exec(s, m, msglist)
}

func activePrefix(channelID string, s *discordgo.Session) (prefix string, err error) {
	prefix = config.Prefix
	_, err = guildDetails(channelID, "", s)
	if err != nil {
		s.ChannelMessageSend(channelID, "There was an issue executing the command :( Try again please~")
		return
	}
	return prefix, nil
}

func (c command) add() command {
	activeCommands[strings.ToLower(c.Name)] = c
	return c
}

func codeBlock(s ...string) string {
	return "```" + strings.Join(s, " ") + "```"
}

// C O M M A N D S
func getCreationTime(ID string) (t time.Time, err error) {
	i, err := strconv.ParseInt(ID, 10, 64)
	if err != nil {
		return
	}

	timestamp := (i >> 22) + 1420070400000
	t = time.Unix(timestamp/1000, 0)
	return
}

func cmdInfo(s *discordgo.Session, m *discordgo.MessageCreate, msglist []string) {
	s.ChannelTyping(m.ChannelID)
	ct1, _ := getCreationTime(s.State.User.ID)
	creationTime := ct1.Format("2006-01-02 3:4:5 pm")

	runtime.ReadMemStats(&mem)
	s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Color: 0x00ff00,
		Title: "Clash Royale Player Stats - Bot",
		Fields: []*discordgo.MessageEmbedField{
			{Name: "API", Value: codeBlock("Custom API from Offical Clash Royale Developer"), Inline: true},
			{Name: "API Access", Value: codeBlock("Contact -> Zile42O#0420"), Inline: true},
			{Name: "Bot Name:", Value: codeBlock(s.State.User.Username), Inline: true},
			{Name: "Creator:", Value: codeBlock("Zile42O#0420"), Inline: true},
			{Name: "Creation Date:", Value: codeBlock(creationTime), Inline: true},
			{Name: "Global Prefix:", Value: codeBlock(config.Prefix), Inline: true},
			{Name: "Programming Language:", Value: codeBlock("Go"), Inline: true},
			{Name: "Library:", Value: codeBlock("DiscordGo"), Inline: true},
			{Name: "Guilds (Servers):", Value: codeBlock(fmt.Sprint("", countGuild)), Inline: true},
			{Name: "Memory Usage:", Value: codeBlock(strconv.Itoa(int(mem.Alloc/1024/1024)) + "MB"), Inline: true},
			{Name: "42O's discord:", Value: "discord.gg/42o or discord.420-clan.com"},
		},
	})
}

func (c command) helpCommand(s *discordgo.Session, m *discordgo.MessageCreate) {
	s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Color: 0x00ff00,

		Fields: []*discordgo.MessageEmbedField{
			{
				Name:  c.Name,
				Value: c.Help,
			},
		},

		Footer: footer,
	})
}
func cmdStats(s *discordgo.Session, m *discordgo.MessageCreate, msglist []string) {
	s.ChannelTyping(m.ChannelID)
	if len(msglist) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xff0000,
			Description: fmt.Sprintf("You need to input a player name!"),
			Footer:      footer,
		})
		return
	}
	url := ""
	text := strings.Join(msglist[1:], " ")
	text = strings.Trim(fmt.Sprintf("%s\n", text), "[]")
	result := strings.Split(fmt.Sprintf("%s", text), ",")
	if len(result) < 2 {
		text := strings.Join(msglist[1:], " ")
		url = fmt.Sprintf("https://420-clan.com/zile42o/v2/clashroyale_api.php?name=%s", f.PathEscape(text))
	} else {
		result[1] = strings.Trim(result[1], " ")
		url = fmt.Sprintf("https://420-clan.com/zile42o/v2/clashroyale_api.php?name=%s&trophies=%s", f.PathEscape(result[0]), f.PathEscape(result[1]))
	}
	res, err := http.Get(url)
	if err != nil {
		errorln("Can't get player from API, err: %s", err)
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xff0000,
			Description: fmt.Sprintf("Can't get player from API!"),
			Footer:      footer,
		})
		return
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		errorln("Can't get player from API, err: %s", err)
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xff0000,
			Description: fmt.Sprintf("Can't get player from API!"),
			Footer:      footer,
		})
		return
	}
	var data interface{}
	err = json.Unmarshal(body, &data)
	if err == nil {

		var name interface{}
		var arena interface{}
		var best_trophies interface{}
		var challenge_maxwins interface{}
		var wins interface{}
		var exp_level interface{}
		var clan interface{}
		var tag interface{}
		var trophies interface{}
		var message []string
		eachJsonValue(&data, func(key *string, index *int, value *interface{}) {

			if key != nil { // It's an object key/value pair...
				if *key == "name" {
					name = *value
				}
				if *key == "arena" {
					arena = *value
				}
				if *key == "best_trophies" {
					best_trophies = *value
				}
				if *key == "challenge_maxwins" {
					challenge_maxwins = *value
				}
				if *key == "wins" {
					wins = *value
				}
				if *key == "exp_level" {
					exp_level = *value
				}
				if *key == "clan" {
					clan = *value
				}
				if *key == "tag" {
					tag = *value
				}
				if *key == "trophies" {
					trophies = *value
				}
			}
		})
		message = append(message, fmt.Sprintf("Name: `%s`\nTag: `%s`\nArena: `%s`\nTrophies: `%.1f`\nBest trophies: `%.1f`\nChallange max wins: `%.1f`\nWins: `%.1f`\nExp level: `%.1f`\nClan: `%s`", name, tag, arena, trophies, best_trophies, challenge_maxwins, wins, exp_level, clan))
		text = strings.Trim(fmt.Sprintf("%s\n", message), "[]")
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0x00ff00,
			Description: fmt.Sprintf("%s", text),
			Footer:      footer,
		})
	} else {
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xff0000,
			Description: fmt.Sprintf("Sorry, can't find that player!"),
			Footer:      footer,
		})
		return
	}
}
func cmdDeck(s *discordgo.Session, m *discordgo.MessageCreate, msglist []string) {
	s.ChannelTyping(m.ChannelID)
	if len(msglist) < 2 {
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xff0000,
			Description: fmt.Sprintf("You need to input a player name!"),
			Footer:      footer,
		})
		return
	}
	url := ""
	text := strings.Join(msglist[1:], " ")
	text = strings.Trim(fmt.Sprintf("%s\n", text), "[]")
	result := strings.Split(fmt.Sprintf("%s", text), ",")
	if len(result) < 2 {
		text := strings.Join(msglist[1:], " ")
		url = fmt.Sprintf("https://420-clan.com/zile42o/v2/clashroyale_api.php?name=%s", f.PathEscape(text))
	} else {
		result[1] = strings.Trim(result[1], " ")
		url = fmt.Sprintf("https://420-clan.com/zile42o/v2/clashroyale_api.php?name=%s&trophies=%s", f.PathEscape(result[0]), f.PathEscape(result[1]))
	}
	//println(url)
	res, err := http.Get(url)
	if err != nil {
		errorln("Can't get player from API, err: %s", err)
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xff0000,
			Description: fmt.Sprintf("Can't get player from API!"),
			Footer:      footer,
		})
		return
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		errorln("Can't get player from API, err: %s", err)
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xff0000,
			Description: fmt.Sprintf("Can't get player from API!"),
			Footer:      footer,
		})
		return
	}
	var data interface{}
	err = json.Unmarshal(body, &data)
	if err == nil {
		var all_deckCards []string
		var all_playerNames []string
		//name := "Unknown"
		eachJsonValue(&data, func(key *string, index *int, value *interface{}) {
			if key != nil { // It's an object key/value pair...
				if *key == "name" {
					all_playerNames = append(all_playerNames, fmt.Sprintf("%s ", *value))
				}
				if *key == "card" {
					all_deckCards = append(all_deckCards, fmt.Sprintf("`%s`, ", *value))
				}
			}
		})
		newstring1 := strings.Trim(fmt.Sprintf("%s", all_deckCards), "[]")
		newstring2 := strings.Trim(fmt.Sprintf("%s", all_playerNames), "[]")
		if len(all_playerNames) > 1 {
			for g, names := range all_playerNames {
				g++
				//println(g)
				var message []string
				var deck []string
				for i, dc := range all_deckCards {
					if i < (g*8) && i >= (g*8)-8 {
						deck = append(deck, fmt.Sprintf("%s", dc))
					}
				}
				newstring := strings.Trim(fmt.Sprintf("%s", deck), "[]")
				message = append(message, fmt.Sprintf("**%s** - %s\n", names, newstring))
				newstring = strings.Trim(fmt.Sprintf("%s", message), "[]")
				s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
					Color: 0x00ff00,
					Title: fmt.Sprintf("Current deck of players (%d/%d)", g, len(all_playerNames)),
					Fields: []*discordgo.MessageEmbedField{
						{Name: "Result:", Value: fmt.Sprintf("%s", newstring), Inline: false},
					},
				})
			}
		} else if len(all_playerNames) <= 1 && len(all_playerNames) > 0 {
			s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
				Color: 0x00ff00,
				Title: "Current deck of player",
				Fields: []*discordgo.MessageEmbedField{
					{Name: "Player", Value: newstring2, Inline: true},
					{Name: "Cards", Value: newstring1, Inline: true},
				},
			})
		} else if len(all_playerNames) < 1 {
			s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
				Color:       0xff0000,
				Description: fmt.Sprintf("Sorry, can't find that player!"),
				Footer:      footer,
			})
		}
	} else {
		s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
			Color:       0xff0000,
			Description: fmt.Sprintf("Sorry, can't find that player!"),
			Footer:      footer,
		})
		return
	}
}

func eachJsonValue(obj *interface{}, handler func(*string, *int, *interface{})) {
	if obj == nil {
		return
	}
	// Yield all key/value pairs for objects.
	o, isObject := (*obj).(map[string]interface{})
	if isObject {
		for k, v := range o {
			handler(&k, nil, &v)
			eachJsonValue(&v, handler)
		}
	}
	// Yield each index/value for arrays.
	a, isArray := (*obj).([]interface{})
	if isArray {
		for i, x := range a {
			handler(nil, &i, &x)
			eachJsonValue(&x, handler)
		}
	}
	// Do nothing for primitives since the handler got them.
}

func cmdHelp(s *discordgo.Session, m *discordgo.MessageCreate, msglist []string) {
	s.ChannelTyping(m.ChannelID)
	if len(msglist) == 2 {
		if val, ok := activeCommands[strings.ToLower(msglist[1])]; ok {
			val.helpCommand(s, m)
			return
		}
	}

	var commands []string
	for _, val := range activeCommands {
		if m.Author.ID == config.OwnerID || !val.OwnerOnly {
			commands = append(commands, "`"+val.Name+"`")
		}
	}

	prefix := config.Prefix
	s.ChannelMessageSendEmbed(m.ChannelID, &discordgo.MessageEmbed{
		Color: 0x00ff00,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Clash Royale Stats Bot Help", Value: strings.Join(commands, ", ") + "\n\nUse `" + prefix + "help [command]` for detailed info about a command."},
		},
	})
}
