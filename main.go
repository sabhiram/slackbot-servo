package main

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/nlopes/slack"
	rpio "github.com/sabhiram/go-rpio"
)

const (
	cFreqMultiplier     = 200 // 50hz but in 200 increments to get 10
	cCenterAngleDegrees = 90.0
	cAngleDelta         = 180.0 / (20.0 - 10.0)
	cInterpDuration     = 200 * time.Millisecond
	cHelpMessage        = "I am a servo control bot! You can tell me to `turn left`,`turn right`, `center`, or ask me for my current `angle`. You can even say things like `full left` or `full right`."
)

////////////////////////////////////////////////////////////////////////////////

func fatalOnErr(err error) {
	if err != nil {
		fmt.Printf("Fatal error: %s\n", err.Error())

		os.Exit(1)
	}
}

func clampAngle(angle float32) float32 {
	if angle < 0.0 {
		angle = 0.0
	} else if angle > 180.0 {
		angle = 180.0
	}
	return angle
}

////////////////////////////////////////////////////////////////////////////////

type cmdFunc func(rtm *slack.RTM, ev *slack.MessageEvent) error

////////////////////////////////////////////////////////////////////////////////

type servo struct {
	pin    rpio.Pin
	angle  float32
	target float32
}

func newServo(bcmpid uint8) (*servo, error) {
	p := rpio.Pin(bcmpid)
	p.Mode(rpio.Pwm)
	p.Freq(50 * cFreqMultiplier)
	s := &servo{
		pin:    p,
		angle:  cCenterAngleDegrees,
		target: cCenterAngleDegrees,
	}
	s.setAngle(cCenterAngleDegrees)
	return s, nil
}

func (s *servo) setTarget(tangle float32) error {
	s.target = clampAngle(tangle)
	return nil
}

// setAngle sets the servo angle to between 0 and 180 degrees.
func (s *servo) setAngle(angle float32) error {
	angle = clampAngle(angle)

	// DutyCycle of 1.0ms / 20ms corresponds to 0 deg
	// 				1.5ms / 20ms corresponds to 90 deg
	//				2.0ms / 20ms corresponds to 180 deg
	dc := uint32(((1.0 + (angle / 180.0)) / 20.0) * cFreqMultiplier)
	s.pin.DutyCycle(dc, cFreqMultiplier)
	s.angle = angle
	return nil
}

func (s *servo) reply(msg string, rtm *slack.RTM, ev *slack.MessageEvent) error {
	rtm.SendMessage(rtm.NewOutgoingMessage(msg, ev.Channel))
	return nil
}

func (s *servo) randomReply(rtm *slack.RTM, ev *slack.MessageEvent) error {
	replies := []string{
		"Umm ok, I can do that for you!",
		"You must be management, snooping around.",
		"Looking for waldo? Let me see what I can do.",
		"Getting right on that boss!",
	}
	return s.reply(replies[rand.Intn(len(replies))], rtm, ev)
}

func (s *servo) errorReply(rtm *slack.RTM, ev *slack.MessageEvent) error {
	replies := []string{
		"Not sure I know what you mean. Type `help` and such.",
		"You must be looking for the `help`?",
		"Are you sure that is a valid command?",
	}
	return s.reply(replies[rand.Intn(len(replies))], rtm, ev)
}

func (s *servo) turnLeft(rtm *slack.RTM, ev *slack.MessageEvent) error {
	if err := s.setAngle(s.angle - cAngleDelta); err != nil {
		return err
	}
	return s.randomReply(rtm, ev)
}

func (s *servo) turnRight(rtm *slack.RTM, ev *slack.MessageEvent) error {
	if err := s.setAngle(s.angle + cAngleDelta); err != nil {
		return err
	}
	return s.randomReply(rtm, ev)
}

func (s *servo) goto0(rtm *slack.RTM, ev *slack.MessageEvent) error {
	if err := s.setTarget(0.0); err != nil {
		return err
	}
	return s.randomReply(rtm, ev)
}

func (s *servo) gotoCenter(rtm *slack.RTM, ev *slack.MessageEvent) error {
	if err := s.setTarget(cCenterAngleDegrees); err != nil {
		return err
	}
	return s.randomReply(rtm, ev)
}

func (s *servo) goto180(rtm *slack.RTM, ev *slack.MessageEvent) error {
	if err := s.setTarget(180.0); err != nil {
		return err
	}
	return s.randomReply(rtm, ev)
}

func (s *servo) getAngle(rtm *slack.RTM, ev *slack.MessageEvent) error {
	s.reply(fmt.Sprintf("Current angle: % .2f°", s.angle), rtm, ev)
	return nil
}

func (s *servo) sendHelp(rtm *slack.RTM, ev *slack.MessageEvent) error {
	return s.reply(cHelpMessage, rtm, ev)
	return nil
}

////////////////////////////////////////////////////////////////////////////////

func main() {
	token := os.Getenv("SLACKBOT_TOKEN")
	if token == "" {
		fatalOnErr(errors.New(`"SLACKBOT_TOKEN" env value missing`))
	}

	fatalOnErr(rpio.Open())
	defer rpio.Close()

	servo, err := newServo(19)
	fatalOnErr(err)

	commands := map[string]cmdFunc{
		"turn left":  servo.turnLeft,
		"turn right": servo.turnRight,
		"full left":  servo.goto0,
		"center":     servo.gotoCenter,
		"full right": servo.goto180,
		"angle":      servo.getAngle,
		"help":       servo.sendHelp,
	}

	api := slack.New(token)
	rtm := api.NewRTM()
	go rtm.ManageConnection()
	ticker := time.NewTicker(cInterpDuration)

Loop:
	for {
		select {
		case msg := <-rtm.IncomingEvents:
			switch evtt := msg.Data.(type) {
			case *slack.MessageEvent:
				text := strings.TrimSpace(strings.ToLower(evtt.Text))
				match := false
				for k, fn := range commands {
					if matched, _ := regexp.MatchString(k, text); matched {
						fn(rtm, evtt)
						match = true
					}
				}
				if !match {
					servo.errorReply(rtm, evtt)
				}
			case *slack.RTMError:
				fmt.Printf("Error: %s\n", evtt.Error())
			case *slack.InvalidAuthEvent:
				fmt.Printf("Bad credentials\n")
				break Loop
			}
		case <-ticker.C:
			if servo.target > servo.angle {
				servo.setAngle(servo.angle + cAngleDelta)
			} else if servo.target < servo.angle {
				servo.setAngle(servo.angle - cAngleDelta)
			}
		}
	}
}

func init() {
	rand.Seed(time.Now().Unix())
}
