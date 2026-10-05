package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"core/shared/config"
	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"core/shared/serverapi"
	"core/shared/sessionenv"
)

type questionHistoryRemote interface {
	SubscribeQuestionHistory(context.Context, *sessionpb.QuestionHistorySubscribeRequest) (serverapi.QuestionHistorySubscription, error)
}

func (c questionCommand) listSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := newCommandFlagSet(config.Command+" questions list", stderr, questionListUsage)
	var sessionFlag *string
	registerOptionalStringFlag(fs, "session", "Session whose answered Questions to list", &sessionFlag)
	maxHandoffs := fs.Int("max-handoffs", 25, "maximum history windows, including the current unfinished window")
	jsonMode := fs.Bool("json", false, "stream structured JSON output")
	if ok, exitCode := parseCommandFlags(fs, args); !ok {
		return exitCode
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(stderr, "questions list does not accept positional arguments")
		return 2
	}
	if *maxHandoffs < 1 {
		fmt.Fprintln(stderr, "--max-handoffs must be at least 1")
		return 2
	}
	handoffs, err := protoapi.Int32(*maxHandoffs, "max handoffs")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	rawSessionID := ""
	if sessionFlag != nil {
		rawSessionID = *sessionFlag
	} else if current, ok := sessionenv.LookupSessionID(os.LookupEnv); ok {
		rawSessionID = current
	}
	if strings.TrimSpace(rawSessionID) == "" {
		fmt.Fprintln(stderr, "Session ID is required")
		return 2
	}
	sessionID, err := parseCLILiveSessionID(rawSessionID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return c.withRemote(stderr, sessionID, func(remote questionCommandRemote) int {
		historyRemote, ok := remote.(questionHistoryRemote)
		if !ok {
			fmt.Fprintln(stderr, "Question-history remote is unavailable")
			return 1
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		sub, err := historyRemote.SubscribeQuestionHistory(ctx, &sessionpb.QuestionHistorySubscribeRequest{
			SessionId: sessionID.String(), MaxHandoffs: handoffs,
		})
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer sub.Close()
		if *jsonMode {
			return streamQuestionHistoryJSON(ctx, sub, stdout, stderr)
		}
		return streamQuestionHistoryHuman(ctx, sub, stdout, stderr)
	})
}

func streamQuestionHistoryHuman(
	ctx context.Context,
	sub serverapi.QuestionHistorySubscription,
	stdout io.Writer,
	stderr io.Writer,
) int {
	wroteBlock := false
	questions := 0
	writeBlock := func(write func() error) error {
		if wroteBlock {
			if _, err := fmt.Fprintln(stdout); err != nil {
				return err
			}
		}
		if err := write(); err != nil {
			return err
		}
		wroteBlock = true
		return nil
	}
	reportWriteFailure := func(err error) int {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for {
		event, err := sub.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0
			}
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(stderr, "Interrupted")
				return 130
			}
			fmt.Fprintln(stderr, err)
			return 1
		}
		switch selected := event.Event.(type) {
		case *sessionpb.QuestionHistoryEvent_Started:
			if selected.Started.LargeHistory {
				if err := writeBlock(func() error {
					_, err := fmt.Fprintln(stdout, "[Session history is large, this command may take a while to finish]")
					return err
				}); err != nil {
					return reportWriteFailure(err)
				}
			}
		case *sessionpb.QuestionHistoryEvent_Question:
			if selected.Question == nil {
				fmt.Fprintln(stderr, "Question-history question event is missing its Question")
				return 1
			}
			questions++
			if err := writeBlock(func() error {
				return writeQuestionHistoryHumanItem(stdout, selected.Question)
			}); err != nil {
				return reportWriteFailure(err)
			}
		case *sessionpb.QuestionHistoryEvent_Completed:
			if questions == 0 {
				if err := writeBlock(func() error {
					_, err := fmt.Fprintln(stdout, "No answered questions found")
					return err
				}); err != nil {
					return reportWriteFailure(err)
				}
			}
			if selected.Completed.HistoryOmitted {
				if err := writeBlock(func() error {
					_, err := fmt.Fprintln(stdout, "[Older Question history omitted; increase --max-handoffs to include more]")
					return err
				}); err != nil {
					return reportWriteFailure(err)
				}
			}
		default:
			fmt.Fprintln(stderr, "Question-history stream returned an unknown event")
			return 1
		}
	}
}

func writeQuestionHistoryHumanItem(stdout io.Writer, question *sessionpb.QuestionHistoryQuestion) error {
	if _, err := fmt.Fprintln(stdout, question.Question); err != nil {
		return err
	}
	if question.SelectedOptionNumber != nil {
		if _, err := fmt.Fprintf(stdout, "Answer: %d. %s\n", *question.SelectedOptionNumber, question.Answer); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintf(stdout, "Answer: %s\n", question.Answer); err != nil {
		return err
	}
	if question.Commentary != nil {
		if _, err := fmt.Fprintf(stdout, "Commentary: %s\n", *question.Commentary); err != nil {
			return err
		}
	}
	if question.CommittedAt != nil {
		at := question.CommittedAt.AsTime().Local()
		if _, err := fmt.Fprintf(stdout, "At: %s\n", at.Format("2006-01-02 15:04:05")); err != nil {
			return err
		}
	}
	return nil
}

func streamQuestionHistoryJSON(
	ctx context.Context,
	sub serverapi.QuestionHistorySubscription,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if _, err := io.WriteString(stdout, `{"questions":[`); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	first := true
	var historyOmitted *bool
	for {
		event, err := sub.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if historyOmitted == nil {
					fmt.Fprintln(stderr, "Question-history stream ended without completion metadata")
					return 1
				}
				if _, err := fmt.Fprintf(stdout, `],"history_omitted":%t}`+"\n", *historyOmitted); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
				return 0
			}
			if errors.Is(err, context.Canceled) {
				fmt.Fprintln(stderr, "Interrupted")
				return 130
			}
			fmt.Fprintln(stderr, err)
			return 1
		}
		switch selected := event.Event.(type) {
		case *sessionpb.QuestionHistoryEvent_Started:
		case *sessionpb.QuestionHistoryEvent_Question:
			question := selected.Question
			if question == nil {
				fmt.Fprintln(stderr, "Question-history question event is missing its Question")
				return 1
			}
			if !first {
				if _, err := io.WriteString(stdout, ","); err != nil {
					fmt.Fprintln(stderr, err)
					return 1
				}
			}
			projected := questionHistoryJSONRecord{
				Question:             question.Question,
				Answer:               question.Answer,
				SelectedOptionNumber: question.SelectedOptionNumber,
				Commentary:           question.Commentary,
			}
			if question.CommittedAt != nil {
				at := question.CommittedAt.AsTime().UTC()
				projected.At = &at
			}
			encoded, err := json.Marshal(projected)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			if _, err := stdout.Write(encoded); err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			first = false
		case *sessionpb.QuestionHistoryEvent_Completed:
			historyOmitted = &selected.Completed.HistoryOmitted
		default:
			fmt.Fprintln(stderr, "Question-history stream returned an unknown event")
			return 1
		}
	}
}

type questionHistoryJSONRecord struct {
	Question             string     `json:"question"`
	Answer               string     `json:"answer"`
	SelectedOptionNumber *int32     `json:"selected_option_number"`
	Commentary           *string    `json:"commentary"`
	At                   *time.Time `json:"at"`
}
