package main

import (
	"errors"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/m1ngsama/filebox/internal/db"
)

// userCmd manages who may reach what. A running server notices a change within 30 seconds.
func userCmd(args []string) error {
	fl := flag.NewFlagSet("user", flag.ExitOnError)
	data := fl.String("data", "./data", "data directory")
	fl.Parse(args)
	rest := fl.Args()
	if len(rest) == 0 {
		return errors.New(usage)
	}
	d, err := openDB(*data)
	if err != nil {
		return err
	}
	defer d.Close()
	switch rest[0] {
	case "ls":
		as, err := d.Accounts()
		if err != nil {
			return err
		}
		for _, a := range as {
			var vs []string
			for v, l := range a.Access {
				vs = append(vs, v+"="+l)
			}
			sort.Strings(vs)
			fmt.Printf("%s\t%s\t%s\n", a.Name, a.Role, strings.Join(vs, " "))
		}
	case "role":
		if len(rest) != 3 || (rest[2] != db.RoleAdmin && rest[2] != db.RoleMember) {
			return errors.New("user role NAME admin|member")
		}
		u, err := d.UserByName(rest[1])
		if err != nil {
			return fmt.Errorf("user %q: %w", rest[1], err)
		}
		if err := d.SetRole(u.ID, rest[2]); errors.Is(err, db.ErrLastAdmin) {
			return errors.New("there must be at least one admin")
		} else if err != nil {
			return err
		}
	case "access":
		if len(rest) != 4 {
			return errors.New("user access NAME VOLUME none|r|rw")
		}
		level := rest[3]
		if level == "none" {
			level = ""
		} else if !db.ValidLevel(level) {
			return errors.New("level must be none, r or rw")
		}
		u, err := d.UserByName(rest[1])
		if err != nil {
			return fmt.Errorf("user %q: %w", rest[1], err)
		}
		return d.SetAccess(u.ID, rest[2], level)
	case "rm":
		if len(rest) != 2 {
			return errors.New("user rm NAME")
		}
		u, err := d.UserByName(rest[1])
		if err != nil {
			return fmt.Errorf("user %q: %w", rest[1], err)
		}
		if err := d.DeleteUser(u.ID); errors.Is(err, db.ErrLastAdmin) {
			return errors.New("there must be at least one admin")
		} else if err != nil {
			return err
		}
	default:
		return errors.New(usage)
	}
	return nil
}
