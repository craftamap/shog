package main

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"
	lm "github.com/charmbracelet/wish/logging"
	"github.com/gliderlabs/ssh"
	"github.com/yuin/goldmark"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/parser"
)

const (
	glamourMaxWidth = 120
)

const (
	ColorActive  = lipgloss.Color("63")
	ColorPassive = lipgloss.Color("8")
)

type SelectedBox int

const (
	BoxNav SelectedBox = iota
	BoxContent
)

type model struct {
	menuItems        []menuItem
	selectedMenuItem int
	width            int
	sidebarWidth     int
	contentWidth     int
	height           int
	selectedBox      SelectedBox

	viewport viewport.Model
}

type menuItem struct {
	title  string
	body   menuBody
	weight int
}

type menuBody struct {
	author  string
	date    time.Time
	content string
}

//go:embed pages
var pages embed.FS

func newModel(menuItems []menuItem) model {

	return model{
		selectedMenuItem: 0,
		menuItems:        menuItems,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m *model) updateSizes(msg tea.Msg) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		minWidth := 20
		maxWidth := 25

		m.width = msg.Width
		m.height = msg.Height
		// calculate SidebarWidth
		sidebarWidth := int(float64(m.width) * 0.2)
		if sidebarWidth < minWidth {
			sidebarWidth = minWidth
		}
		if sidebarWidth > maxWidth {
			sidebarWidth = maxWidth
		}
		m.sidebarWidth = sidebarWidth
		m.contentWidth = m.width - sidebarWidth
	}
}

func (m *model) updateContent() {
	c := m.menuItems[m.selectedMenuItem].body.content
	md, _ := m.glamourize(c)
	m.viewport.SetContent(md)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	m.updateSizes(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.selectedBox == BoxNav {
			switch msg.String() {
			case tea.KeyDown.String():
				m.selectedMenuItem = (m.selectedMenuItem + 1) % len(m.menuItems)
				m.viewport.GotoTop()
			case tea.KeyUp.String():
				// TODO: Shouldn't this be possible with math?
				m.selectedMenuItem = m.selectedMenuItem - 1
				if (m.selectedMenuItem) < 0 {
					m.selectedMenuItem = len(m.menuItems) - 1
				}
				m.viewport.GotoTop()
			}
			m.updateContent()
		} else if m.selectedBox == BoxContent {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		switch msg.String() {

		case "ctrl+c", "q":
			return m, tea.Quit
		case tea.KeyRight.String():
			m.selectedBox = BoxContent
		case tea.KeyLeft.String():
			m.selectedBox = BoxNav
		}
	// case tea.WindowSizeMsg:
	// 	nil
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	}

	m.updateContent()

	return m, tea.Batch(cmds...)
}

const banner = `` +
	`
╔═╗┌─┐┌┐ ┬┌─┐┌┐┌
╠╣ ├─┤├┴┐│├─┤│││
╚  ┴ ┴└─┘┴┴ ┴┘└┘
╔═╗┬┌─┐┌─┐┌─┐┬  
╚═╗│├┤ │ ┬├┤ │  
╚═╝┴└─┘└─┘└─┘┴─┘
`

func (m model) viewSidebar() string {
	clr := ColorPassive
	if m.selectedBox == BoxNav {
		clr = ColorActive
	}
	sidebarStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(clr).Padding(0, 1).Align(lipgloss.Center)

	rWidth := m.sidebarWidth - sidebarStyle.GetVerticalBorderSize() // border ignores width => we really use width, but we want our content to take rWidth

	selectedItemStyle := lipgloss.NewStyle().Background(clr)

	var sb strings.Builder
	for i, mI := range m.menuItems {
		title := mI.title
		if i == m.selectedMenuItem {
			title = selectedItemStyle.Render(mI.title)
		}
		sb.WriteString(title)
		sb.WriteString("\n")
	}

	s := lipgloss.JoinVertical(0, banner, sb.String())
	return sidebarStyle.Width(rWidth).Render(s)
}

func (m model) viewContentFooter() (string, int) {
	borderColor := ColorPassive
	if m.selectedBox == BoxContent {
		borderColor = ColorActive
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1)
	secondaryTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	body := style.Width(m.contentWidth - style.GetHorizontalBorderSize()).Render(secondaryTextStyle.Render("Author: ") + m.menuItems[m.selectedMenuItem].body.author + secondaryTextStyle.Render(" Date: ") + m.menuItems[m.selectedMenuItem].body.date.Format("2006-01-02"))
	return body, lipgloss.Height(body)
}

func (m *model) glamourize(md string) (string, error) {
	w := m.contentWidth - 8
	if w > glamourMaxWidth {
		w = glamourMaxWidth
	}
	tr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(w),
	)

	if err != nil {
		return "", err
	}
	mdt, err := tr.Render(md)
	if err != nil {
		return "", err
	}
	mdt = lipgloss.NewStyle().MaxWidth(w).Render(mdt)
	return mdt, nil
}

func (m model) viewContent() string {
	borderColor := lipgloss.Color("8")
	if m.selectedBox == BoxContent {
		borderColor = lipgloss.Color("63")
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor)
	footer, footerHeight := m.viewContentFooter()
	m.viewport.Height = (m.height - style.GetVerticalBorderSize()) - footerHeight
	m.viewport.Width = m.contentWidth - style.GetHorizontalFrameSize()

	return lipgloss.JoinVertical(0, style.Width(m.contentWidth-style.GetHorizontalFrameSize()).Render(m.viewport.View()), footer)
}

func (m model) View() string {
	sidebar := m.viewSidebar()
	content := m.viewContent()
	return lipgloss.JoinHorizontal(0, sidebar, content)
}

const host = "localhost"
const port = 23234

func main() {
	log.Println("Starting up shog server...")
	teaHandler, err := buildTeaHandler()
	if err != nil {
		log.Fatalln(err)
	}
	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf("%s:%d", host, port)),
		wish.WithHostKeyPath(".ssh/term_info_ed25519"),
		wish.WithMiddleware(
			activeterm.Middleware(),
			bm.Middleware(teaHandler),
			lm.Middleware(),
		),
	)
	if err != nil {
		log.Fatalln(err)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Printf("Starting SSH server on %s:%d", host, port)
	go func() {
		if err = s.ListenAndServe(); err != nil {
			log.Fatalln(err)
		}
	}()

	<-done
	log.Println("Stopping SSH server")
	if err := s.Close(); err != nil {
		log.Fatalln(err)
	}
}

func readPages() ([]menuItem, error) {
	dirEntries, err := pages.ReadDir("pages")
	if err != nil {
		return nil, err
	}

	markdown := goldmark.New(
		goldmark.WithExtensions(
			meta.Meta,
		),
	)

	var menuItems []menuItem
	for _, dirEntry := range dirEntries {
		log.Printf("Found page %s", dirEntry.Name())
		pageBytes, err := fs.ReadFile(pages, path.Join("pages", dirEntry.Name()))
		if err != nil {
			panic(err)
		}
		var buf bytes.Buffer
		context := parser.NewContext()
		if err := markdown.Convert(pageBytes, &buf, parser.WithContext(context)); err != nil {
			return nil, err
		}
		metaData := meta.Get(context)
		// TODO: find good solution for this
		content := strings.SplitN(string(pageBytes), "---", 3)[2]

		parsedDate, _ := time.Parse("2006-01-02", metaData["date"].(string))

		menuItems = append(menuItems, menuItem{
			title:  metaData["title"].(string),
			weight: metaData["weight"].(int),
			body: menuBody{
				author:  metaData["author"].(string),
				content: content,
				date:    parsedDate,
			},
		})
	}

	sort.Slice(menuItems[:], func(i, j int) bool {
		return menuItems[i].weight < menuItems[j].weight
	})

	return menuItems, nil
}

func buildTeaHandler() (func(ssh.Session) (tea.Model, []tea.ProgramOption), error) {
	pages, err := readPages()
	if err != nil {
		return nil, err
	}

	return func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
		pty, _, active := s.Pty()
		if !active {
			fmt.Println("no active terminal, skipping")
			return nil, nil
		}

		m := newModel(pages)
		m.width = pty.Window.Width
		m.height = pty.Window.Height
		return m, []tea.ProgramOption{tea.WithAltScreen()}
	}, nil
}
