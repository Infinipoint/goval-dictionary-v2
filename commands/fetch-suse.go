package commands

import (
	"context"
	"flag"
	"os"
	"strings"
	"time"

	"github.com/google/subcommands"
	c "github.com/kotakanbe/goval-dictionary/config"
	"github.com/kotakanbe/goval-dictionary/db"
	"github.com/kotakanbe/goval-dictionary/fetcher"
	"github.com/kotakanbe/goval-dictionary/log"
	"github.com/kotakanbe/goval-dictionary/models"
	"github.com/kotakanbe/goval-dictionary/util"
)

// FetchSUSECmd is Subcommand for fetch SUSE OVAL
type FetchSUSECmd struct {
	OpenSUSE              bool
	OpenSUSELeap          bool
	SUSEEnterpriseServer  bool
	SUSEEnterpriseDesktop bool
	SUSEOpenstackCloud    bool
	Debug                 bool
	DebugSQL              bool
	LogDir                string
	DBPath                string
	DBType                string
	HTTPProxy             string
}

// Name return subcommand name
func (*FetchSUSECmd) Name() string { return "fetch-suse" }

// Synopsis return synopsis
func (*FetchSUSECmd) Synopsis() string { return "Fetch Vulnerability dictionary from SUSE" }

// Usage return usage
func (*FetchSUSECmd) Usage() string {
	return `fetch-suse:
	fetch-suse
		[-opensuse]
		[-opensuse-leap]
		[-suse-enterprise-server]
		[-suse-enterprise-desktop]
		[-suse-openstack-cloud]
		[-dbtype=mysql|sqlite3]
		[-dbpath=$PWD/cve.sqlite3 or connection string]
		[-http-proxy=http://192.168.0.1:8080]
		[-debug]
		[-debug-sql]
		[-log-dir=/path/to/log]

	example: goval-dictionary fetch-suse -opensuse 13.2

`
}

// SetFlags set flag
func (p *FetchSUSECmd) SetFlags(f *flag.FlagSet) {

	f.BoolVar(&p.OpenSUSE, "opensuse", false, "OpenSUSE")
	f.BoolVar(&p.OpenSUSELeap, "opensuse-leap", false, "OpenSUSE Leap")
	f.BoolVar(&p.SUSEEnterpriseServer, "suse-enterprise-server", false, "SUSE Enterprise Server")
	f.BoolVar(&p.SUSEEnterpriseDesktop, "suse-enterprise-desktop", false, "SUSE Enterprise Desktop")
	f.BoolVar(&p.SUSEEnterpriseDesktop, "suse-openstack-cloud", false, "SUSE Openstack cloud")

	f.BoolVar(&p.Debug, "debug", false, "debug mode")
	f.BoolVar(&p.DebugSQL, "debug-sql", false, "SQL debug mode")

	defaultLogDir := util.GetDefaultLogDir()
	f.StringVar(&p.LogDir, "log-dir", defaultLogDir, "/path/to/log")

	pwd := os.Getenv("PWD")
	f.StringVar(&p.DBPath, "dbpath", pwd+"/oval.sqlite3",
		"/path/to/sqlite3 or SQL connection string")

	f.StringVar(&p.DBType, "dbtype", "sqlite3",
		"Database type to store data in (sqlite3 or mysql supported)")

	f.StringVar(
		&p.HTTPProxy,
		"http-proxy",
		"",
		"http://proxy-url:port (default: empty)",
	)
}

// Execute execute
func (p *FetchSUSECmd) Execute(_ context.Context, f *flag.FlagSet, _ ...interface{}) subcommands.ExitStatus {
	log.Initialize(p.LogDir)

	c.Conf.DebugSQL = p.DebugSQL
	c.Conf.Debug = p.Debug
	if c.Conf.Debug {
		log.SetDebug()
	}

	c.Conf.DBPath = p.DBPath
	c.Conf.DBType = p.DBType
	c.Conf.HTTPProxy = p.HTTPProxy

	if !c.Conf.Validate() {
		return subcommands.ExitUsageError
	}

	vers := []string{}
	if len(f.Args()) == 0 {
		log.Errorf("Specify versions to fetch. Oval are here: http://ftp.suse.com/pub/projects/security/oval/")
		return subcommands.ExitUsageError
	}
	for _, arg := range f.Args() {
		vers = append(vers, arg)
	}

	suseType := ""
	switch {
	case p.OpenSUSE:
		suseType = c.OpenSUSE
	case p.OpenSUSELeap:
		suseType = c.OpenSUSELeap
	case p.SUSEEnterpriseServer:
		suseType = c.SUSEEnterpriseServer
	case p.SUSEEnterpriseDesktop:
		suseType = c.SUSEEnterpriseDesktop
	case p.SUSEOpenstackCloud:
		suseType = c.SUSEOpenstackCloud
	}

	results, err := fetcher.FetchSUSEFiles(suseType, vers)
	if err != nil {
		log.Error(err)
		return subcommands.ExitFailure
	}

	log.Infof("Opening DB (%s).", c.Conf.DBType)
	if err := db.OpenDB(); err != nil {
		log.Error(err)
		return subcommands.ExitFailure
	}

	log.Info("Migrating DB")
	if err := db.MigrateDB(); err != nil {
		log.Error(err)
		return subcommands.ExitFailure
	}

	suse := db.NewSUSE(suseType)
	for _, r := range results {
		log.Infof("Fetched: %s", r.URL)
		log.Infof("  %d OVAL definitions", len(r.Root.Definitions.Definitions))

		defs := models.ConvertSUSEToModel(r.Root)

		var timeformat = "2006-01-02T15:04:05"
		t, err := time.Parse(timeformat, r.Root.Generator.Timestamp)
		if err != nil {
			panic(err)
		}

		root := models.Root{
			Family:      suseType,
			OSVersion:   r.Target,
			Definitions: defs,
		}

		ss := strings.Split(r.URL, "/")
		fmeta := models.FetchMeta{
			Timestamp: t,
			FileName:  ss[len(ss)-1],
		}

		if err := suse.InsertOval(&root, fmeta); err != nil {
			log.Error(err)
			return subcommands.ExitFailure
		}

		if err := suse.InsertFetchMeta(fmeta); err != nil {
			log.Error(err)
			return subcommands.ExitFailure
		}
	}

	return subcommands.ExitSuccess
}