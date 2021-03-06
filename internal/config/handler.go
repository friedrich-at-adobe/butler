/*
Copyright 2017 Adobe. All rights reserved.
This file is licensed to you under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License. You may obtain a copy
of the License at http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software distributed under
the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
OF ANY KIND, either express or implied. See the License for the specific language
governing permissions and limitations under the License.
*/

package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adobe/butler/internal/methods"
	"github.com/adobe/butler/internal/metrics"
	"github.com/adobe/butler/internal/reloaders"

	"github.com/jasonlvhit/gocron"
	log "github.com/sirupsen/logrus"
)

type ButlerConfig struct {
	url                     *url.URL
	Client                  *ConfigClient
	Config                  *ConfigSettings
	FirstRun                bool
	LogLevel                log.Level
	PrevCMSchedulerInterval int
	Interval                int
	Timeout                 int
	RawConfig               []byte
	Scheduler               *gocron.Scheduler
	InsecureSkipVerify      bool
	MethodOpts              methods.MethodOpts
}

var (
	handlerCounter   = 0
	cmHandlerCounter = 0
)

func (bc *ButlerConfig) SetScheme(s string) error {
	var (
		res error
	)
	scheme := strings.ToLower(s)
	if !IsValidScheme(scheme) {
		errMsg := fmt.Sprintf("%s is an invalid scheme", scheme)
		log.Errorf("Config::SetScheme(): %s is an invalid scheme", scheme)
		res = errors.New(errMsg)
	} else {
		log.Debugf("Config::SetScheme(): setting bc.Scheme=%s", scheme)
		bc.url.Scheme = scheme
	}
	return res
}

func (bc *ButlerConfig) Scheme() string {
	return bc.url.Scheme
}

func (bc *ButlerConfig) SetPath(p string) error {
	newPath := filepath.Clean(p)
	log.Debugf("Config::SetPath(): setting bc.Path=%s", newPath)
	bc.url.Path = newPath
	return nil
}

func (bc *ButlerConfig) SetMethodOpts(opts methods.MethodOpts) error {
	bc.MethodOpts = opts
	return nil
}

func (bc *ButlerConfig) Path() string {
	return bc.url.Path
}

func (bc *ButlerConfig) Host() string {
	return bc.url.Host
}

func (bc *ButlerConfig) URL() *url.URL {
	return bc.url
}

func (bc *ButlerConfig) SetURL(u *url.URL) {
	bc.url = u
}

func (bc *ButlerConfig) Opts() methods.MethodOpts {
	return bc.MethodOpts
}

func (bc *ButlerConfig) SetInterval(t int) error {
	log.Debugf("Config::SetInterval(): setting bc.Interval=%v", t)
	bc.Interval = t
	return nil
}

func (bc *ButlerConfig) GetCMInterval() int {
	return bc.Config.Globals.SchedulerInterval
}

func (bc *ButlerConfig) SetCMInterval(i int) error {
	bc.Config.Globals.SchedulerInterval = i
	return nil
}

func (bc *ButlerConfig) GetCMPrevInterval() int {
	return bc.PrevCMSchedulerInterval
}

func (bc *ButlerConfig) SetCMPrevInterval(i int) error {
	bc.PrevCMSchedulerInterval = i
	return nil
}

func (bc *ButlerConfig) GetInterval() int {
	return bc.Interval
}

func (bc *ButlerConfig) SetTimeout(t int) error {
	log.Debugf("Config::SetTimeout(): setting bc.Timeout=%v", t)
	bc.Timeout = t
	return nil
}

func (bc *ButlerConfig) SetLogLevel(level log.Level) {
	log.SetLevel(level)
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})
	bc.LogLevel = level
	log.Debugf("Config::SetLogLevel(): setting log level to %s", level)
}

func (bc *ButlerConfig) GetLogLevel() log.Level {
	return bc.LogLevel
}

func (bc *ButlerConfig) Init() error {
	log.Infof("Config::Init(): initializing butler config.")
	var err error

	client, err := NewConfigClient(bc)
	if err != nil {
		log.Errorf("Config::Init(): could not initialize butler config. err=%s", err.Error())
		return err
	}

	bc.Client = client
	bc.Config = NewConfigSettings()

	log.Infof("Config::Init(): butler config initialized.")
	return nil
}

func (bc *ButlerConfig) Handler() error {
	log.Infof("ButlerConfig::Handler()[count=%v]: entering.", handlerCounter)
	response, err := bc.Client.Get(bc.URL())

	if err != nil {
		log.Errorf("ButlerConfig::Handler()[count=%v]: Cannot retrieve butler configuration. err=%s", handlerCounter, err.Error())
		log.Errorf("ButlerConfig::Handler()[count=%v]: done.", handlerCounter)
		handlerCounter++
		metrics.SetButlerContactVal(metrics.FAILURE, bc.Host(), bc.Path())
		return err
	}
	defer response.GetResponseBody().Close()

	if response.GetResponseStatusCode() != 200 {
		metrics.SetButlerContactVal(metrics.FAILURE, bc.Host(), bc.Path())
		log.Errorf("ButlerConfig::Handler()[count=%v]: Did not receive 200 response code for %s. code=%d", handlerCounter, bc.URL().String(), response.GetResponseStatusCode())
		log.Errorf("ButlerConfig::Handler()[count=%v] done.", handlerCounter)
		handlerCounter++
		errMsg := fmt.Sprintf("Did not receive 200 response code for %s. code=%d", bc.URL().String(), response.GetResponseStatusCode())
		return errors.New(errMsg)
	}

	body, err := ioutil.ReadAll(response.GetResponseBody())
	if err != nil {
		metrics.SetButlerContactVal(metrics.FAILURE, bc.Host(), bc.Path())
		log.Errorf("ButlerConfig::Handler()[count=%v]: Could not read response body for %s. err=%s", handlerCounter, bc.URL().String(), err)
		log.Errorf("ButlerConfig::Handler()[count=%v] done.", handlerCounter)
		handlerCounter++
		errMsg := fmt.Sprintf("Could not read response body for %s. err=%s", bc.URL().String(), err)
		return errors.New(errMsg)
	}

	err = ValidateConfig(NewValidateOpts().WithData(body).WithFileName("butler.toml").WithManager("butler-config"))
	if err != nil {
		metrics.SetButlerContactVal(metrics.FAILURE, bc.Host(), bc.Path())
		return err
	}

	if bc.RawConfig == nil {
		err := bc.Config.ParseConfig(body)
		if err != nil {
			if bc.Config.Globals.ExitOnFailure {
				log.Fatal(err)
			} else {
				metrics.SetButlerContactVal(metrics.FAILURE, bc.Host(), bc.Path())
				return err
			}
		} else {
			log.Debugf("ButlerConfig::Handler()[count=%v]: bc.RawConfig is nil. Filling it up.", handlerCounter)
			bc.RawConfig = body
		}
	}

	if !bytes.Equal(bc.RawConfig, body) {
		err := bc.Config.ParseConfig(body)
		if err != nil {
			if bc.Config.Globals.ExitOnFailure {
				log.Fatal(err)
			} else {
				metrics.SetButlerContactVal(metrics.FAILURE, bc.Host(), bc.Path())
				return err
			}
		} else {
			log.Infof("ButlerConfig::Handler()[count=%v]: butler config has changed. updating.", handlerCounter)
			bc.RawConfig = body
		}
	} else {
		if !bc.FirstRun {
			log.Infof("ButlerConfig::Handler()[count=%v]: butler config unchanged.", handlerCounter)
		}
	}

	// We don't want to handle the scheduler stuff on the first run. The scheduler doesn't yet exist
	log.Debugf("ButlerConfig::Handler()[count=%v]: CM PrevSchedulerInterval=%v SchedulerInterval=%v", handlerCounter, bc.GetCMPrevInterval(), bc.GetCMInterval())

	// This is going to manage the CM scheduler. If it changes in the butler configuration, we should be aware of it.
	if bc.FirstRun {
		bc.FirstRun = false
	} else {
		// If we need to start the scheduler, then let's do that
		// If PrevInterval == 0, then no scheduler has been started
		if bc.GetCMPrevInterval() == 0 {
			log.Debugf("ButlerConfig::Handler()[count=%v]: starting scheduler for RunCMHandler each %v seconds", handlerCounter, bc.GetCMInterval())
			bc.Scheduler.Every(uint64(bc.GetCMInterval())).Seconds().Do(bc.RunCMHandler)
			bc.SetCMPrevInterval(bc.GetCMInterval())
		}
		// If PrevInterval is > 0 and the Intervals differ, then the configuration has changed.
		// We should restart the scheduler
		if (bc.GetCMPrevInterval() != 0) && (bc.GetCMPrevInterval() != bc.GetCMInterval()) {
			log.Debugf("ButlerConfig::Handler()[count=%v]: butler CM interval has changed from %v to %v", handlerCounter, bc.GetCMPrevInterval(), bc.GetCMInterval())
			log.Debugf("ButlerConfig::Handler()[count=%v]: stopping current butler scheduler for RunCMHandler", handlerCounter)
			bc.Scheduler.Remove(bc.RunCMHandler)
			log.Debugf("ButlerConfig::Handler()[count=%v]: re-starting scheduler for RunCMHandler each %v seconds", handlerCounter, bc.GetCMInterval())
			bc.Scheduler.Every(uint64(bc.GetCMInterval())).Seconds().Do(bc.RunCMHandler)
			bc.SetCMPrevInterval(bc.GetCMInterval())
		}
	}
	metrics.SetButlerContactVal(metrics.SUCCESS, bc.Host(), bc.Path())
	log.Infof("ButlerConfig::Handler()[count=%v]: done.", handlerCounter)
	handlerCounter++
	return nil
}

func (bc *ButlerConfig) SetScheduler(s *gocron.Scheduler) error {
	log.Debugf("Config::SetScheduler(): entering")
	bc.Scheduler = s
	return nil
}

func (bc *ButlerConfig) RunCMHandler() error {
	var (
		ReloadManager []string
	)
	log.Infof("Config::RunCMHandler()[count=%v]: entering.", cmHandlerCounter)

	c1 := make(chan ChanEvent)
	c2 := make(chan ChanEvent)

	bc.CheckPaths()

	for _, m := range bc.GetManagers() {
		go m.DownloadPrimaryConfigFiles(c1)
		go m.DownloadAdditionalConfigFiles(c2)
		PrimaryChan, AdditionalChan := <-c1, <-c2

		if PrimaryChan.CanCopyFiles() && AdditionalChan.CanCopyFiles() {
			log.Debugf("Config::RunCMHandler()[count=%v]: successfully retrieved files. processing...", cmHandlerCounter)
			p := PrimaryChan.CopyPrimaryConfigFiles(m.ManagerOpts)
			a := AdditionalChan.CopyAdditionalConfigFiles(m.DestPath)
			if p || a {
				ReloadManager = append(ReloadManager, m.Name)
			}
			PrimaryChan.CleanTmpFiles()
			AdditionalChan.CleanTmpFiles()
			metrics.SetButlerRemoteRepoUp(metrics.SUCCESS, m.Name)
			metrics.SetButlerRemoteRepoSanity(metrics.SUCCESS, m.Name)
		} else {
			log.Debugf("Config::RunCMHandler()[count=%v]: cannot copy files. cleaning up...", cmHandlerCounter)
			// Failure statistics for RemoteRepoUp and RemoteRepoSanity
			// happen in DownloadPrimaryConfigFiles // DownloadAdditionalConfigFiles
			PrimaryChan.CleanTmpFiles()
			AdditionalChan.CleanTmpFiles()
		}
		m.LastRun = time.Now()
	}

	if len(ReloadManager) == 0 {
		log.Infof("Config::RunCMHandler()[count=%v]: CM files unchanged.", cmHandlerCounter)
		// We are going to run through the managers and ensure that the status file
		// is in an OK state for the manager. If it is not, then we will attempt a reload
		for _, m := range bc.GetManagers() {
			metrics.SetButlerRepoInSync(metrics.SUCCESS, m.Name)
			if !GetManagerStatus(bc.GetStatusFile(), m.Name) {
				log.Debugf("Config::RunCMHandler()[count=%v]: Could not find manager status. Going to reload to get in sync.", cmHandlerCounter)
				err := m.Reload()
				if err != nil {
					switch e := err.(type) {
					case *reloaders.ReloaderError:
						// an http timeout is 1
						log.Debugf("Config::RunCMHandler()[count=%v]: e.Code=%#v, m.ManagerTimeoutOk=%#v", cmHandlerCounter, e.Code, m.ManagerTimeoutOk)
						if e.Code == 1 && m.ManagerTimeoutOk == true {
							// we really don't care about here
							// let's make sure we at least delete our metrics
							metrics.DeleteButlerReloadVal(m.Name)
						} else {
							log.Errorf("Config::RunCMHandler()[count=%v]: err=%#v", cmHandlerCounter, err)
							err := SetManagerStatus(bc.GetStatusFile(), m.Name, false)
							if err != nil {
								log.Fatalf("Config::RunCMHandler()[count=%v]: could not write to %v err=%v", cmHandlerCounter, bc.GetStatusFile(), err.Error())
							}
							metrics.SetButlerReloadVal(metrics.FAILURE, m.Name)
							if m.EnableCache && m.GoodCache {
								RestoreCachedConfigs(m.Name, bc.Config.GetAllConfigLocalPaths(m.Name), m.CleanFiles)
							}
						}
					}
				} else {
					err := SetManagerStatus(bc.GetStatusFile(), m.Name, true)
					if err != nil {
						log.Fatalf("Config::RunCMHandler()[count=%v]: could not write to %v err=%v", cmHandlerCounter, bc.GetStatusFile(), err.Error())
					}
					metrics.SetButlerReloadVal(metrics.SUCCESS, m.Name)
					if m.EnableCache {
						CacheConfigs(m.Name, bc.Config.GetAllConfigLocalPaths(m.Name))
						m.GoodCache = true
					}
				}
			}
		}
	} else {
		log.Debugf("Config::RunCMHandler()[count=%v]: CM files changed... reloading.", cmHandlerCounter)
		for _, m := range ReloadManager {
			log.Debugf("Config::RunCMHandler()[count=%v]: m=%#v", cmHandlerCounter, m)
			mgr := bc.GetManager(m)
			err := mgr.Reload()
			if err != nil {
				switch e := err.(type) {
				case *reloaders.ReloaderError:
					log.Debugf("Config::RunCMHandler()[count=%v]: e.Code=%#v, mgr.ManagerTimeoutOk=%#v", cmHandlerCounter, e.Code, mgr.ManagerTimeoutOk)
					if e.Code == 1 && mgr.ManagerTimeoutOk == true {
						// we really don't care about here, but
						// let's make sure we at least delete our metrics
						metrics.DeleteButlerReloadVal(mgr.Name)
					} else {
						log.Errorf("Config::RunCMHandler()[count=%v]: Could not reload manager \"%v\" err=%#v", cmHandlerCounter, mgr.Name, err)
						err := SetManagerStatus(bc.GetStatusFile(), m, false)
						if err != nil {
							log.Fatalf("Config::RunCMHandler()[count=%v]: could not write to %v err=%v", cmHandlerCounter, bc.GetStatusFile(), err.Error())
						}
						metrics.SetButlerReloadVal(metrics.FAILURE, m)
						if mgr.EnableCache && mgr.GoodCache {
							RestoreCachedConfigs(m, bc.Config.GetAllConfigLocalPaths(mgr.Name), mgr.CleanFiles)
						}
					}
				}
			} else {
				err := SetManagerStatus(bc.GetStatusFile(), m, true)
				if err != nil {
					log.Fatalf("Config::RunCMHandler()[count=%v]: could not write to %v err=%v", cmHandlerCounter, bc.GetStatusFile(), err.Error())
				}
				metrics.SetButlerReloadVal(metrics.SUCCESS, m)
				if mgr.EnableCache {
					CacheConfigs(m, bc.Config.GetAllConfigLocalPaths(mgr.Name))
					mgr.GoodCache = true
				}
			}
		}
	}
	log.Infof("Config::RunCMHandler()[count=%v]: done.", cmHandlerCounter)
	cmHandlerCounter++
	return nil
}

func (bc *ButlerConfig) GetManagers() map[string]*Manager {
	return bc.Config.Managers
}

func (bc *ButlerConfig) GetManager(m string) *Manager {
	return bc.Config.Managers[m]
}

func (bc *ButlerConfig) GetStatusFile() string {
	return bc.Config.Globals.StatusFile
}

func (bc *ButlerConfig) CheckPaths() error {
	log.Debugf("Config::CheckPaths(): entering")
	for _, m := range bc.Config.Managers {
		for _, f := range m.GetAllLocalPaths() {
			dir := filepath.Dir(f)
			if _, err := os.Stat(dir); err != nil {
				err = os.MkdirAll(dir, 0755)
				if err != nil {
					msg := fmt.Sprintf("Config::CheckPaths(): err=%s", err.Error())
					log.Fatal(msg)
				}
				log.Infof("Config::CheckPaths(): Created directory \"%s\"", dir)
				log.Debugf("Config::CheckPaths(): setting m.ReloadManager=true")
				m.ReloadManager = true
			}
		}

		if m.CleanFiles {
			err := filepath.Walk(m.DestPath, m.PathCleanup)
			if err != nil {
				log.Debugf("Config::CheckPaths(): got err for filepath. setting m.ReloadManager=true")
				m.ReloadManager = true
			}
		}
	}
	return nil
}
