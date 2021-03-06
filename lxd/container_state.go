package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"github.com/lxc/lxd/shared"
)

type containerStatePutReq struct {
	Action   string `json:"action"`
	Timeout  int    `json:"timeout"`
	Force    bool   `json:"force"`
	Stateful bool   `json:"stateful"`
}

func containerState(d *Daemon, r *http.Request) Response {
	name := mux.Vars(r)["name"]
	c, err := containerLoadByName(d, name)
	if err != nil {
		return SmartError(err)
	}

	state, err := c.RenderState()
	if err != nil {
		return InternalError(err)
	}

	return SyncResponse(true, state)
}

func containerStatePut(d *Daemon, r *http.Request) Response {
	name := mux.Vars(r)["name"]

	raw := containerStatePutReq{}

	// We default to -1 (i.e. no timeout) here instead of 0 (instant
	// timeout).
	raw.Timeout = -1

	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return BadRequest(err)
	}

	// Don't mess with containers while in setup mode
	<-d.readyChan

	c, err := containerLoadByName(d, name)
	if err != nil {
		return SmartError(err)
	}

	var do func(*operation) error
	switch shared.ContainerAction(raw.Action) {
	case shared.Start:
		do = func(op *operation) error {
			if err = c.Start(raw.Stateful); err != nil {
				return err
			}
			return nil
		}
	case shared.Stop:
		if raw.Stateful {
			do = func(op *operation) error {
				err := c.Stop(raw.Stateful)
				if err != nil {
					return err
				}

				return nil
			}
		} else if raw.Timeout == 0 || raw.Force {
			do = func(op *operation) error {
				err = c.Stop(false)
				if err != nil {
					return err
				}

				if c.IsEphemeral() {
					c.Delete()
				}

				return nil
			}
		} else {
			do = func(op *operation) error {
				if c.IsFrozen() {
					err := c.Unfreeze()
					if err != nil {
						return err
					}
				}

				err = c.Shutdown(time.Duration(raw.Timeout) * time.Second)
				if err != nil {
					return err
				}

				if c.IsEphemeral() {
					c.Delete()
				}

				return nil
			}
		}
	case shared.Restart:
		do = func(op *operation) error {
			if raw.Timeout == 0 || raw.Force {
				err = c.Stop(false)
				if err != nil {
					return err
				}
			} else {
				err = c.Shutdown(time.Duration(raw.Timeout) * time.Second)
				if err != nil {
					return err
				}
			}
			err = c.Start(false)
			if err != nil {
				return err
			}

			return nil
		}
	case shared.Freeze:
		do = func(op *operation) error {
			return c.Freeze()
		}
	case shared.Unfreeze:
		do = func(op *operation) error {
			return c.Unfreeze()
		}
	default:
		return BadRequest(fmt.Errorf("unknown action %s", raw.Action))
	}

	resources := map[string][]string{}
	resources["containers"] = []string{name}

	op, err := operationCreate(operationClassTask, resources, nil, do, nil, nil)
	if err != nil {
		return InternalError(err)
	}

	return OperationResponse(op)
}
