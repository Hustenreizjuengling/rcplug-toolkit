package flash

import (
	"errors"
	"fmt"

	"github.com/Hustenreizjuengling/rcplug-toolkit/internal/fwimage"
)

// Check is one item of a pre-flight report.
type Check struct {
	ID     string `json:"id"`             // system, archive, image, device, access, mount, python (Linux), app (Windows)
	OK     bool   `json:"ok"`             // the run can proceed as far as this item goes
	Warn   bool   `json:"warn,omitempty"` // OK, but worth a look before flashing
	Detail string `json:"detail"`         // what was found, or what to do
	err    error
}

func pass(id, detail string) Check { return Check{ID: id, OK: true, Detail: detail} }
func fail(id string, err error) Check {
	return Check{ID: id, Detail: err.Error(), err: err}
}

// Report is what Preflight found. Every check runs on its own, so a UI can
// show everything that is missing at once.
type Report struct {
	Checks  []Check       `json:"checks"`
	Staging *Staging      `json:"staging,omitempty"`
	Image   *fwimage.Info `json:"image,omitempty"`
	Devices []Device      `json:"devices,omitempty"`
	Node    string        `json:"node,omitempty"` // the HID node a run uses
	Python  string        `json:"python,omitempty"`
}

func (r *Report) add(c Check) { r.Checks = append(r.Checks, c) }

// Err returns the first failed check as an error, or nil if a run can go ahead.
func (r *Report) Err() error {
	for _, c := range r.Checks {
		if !c.OK {
			if c.err != nil {
				return c.err
			}
			return errors.New(c.Detail)
		}
	}
	return nil
}

// Preflight checks everything a run needs without sending anything to the
// device: that this system can flash, the staged archive and its checksum,
// that the image is RØDECaster Duo firmware (and whether it carries a key),
// the device on USB and that it is a Duo, access to its HID interface, and
// what the system needs for the update disk (Linux: how it gets mounted, and
// Python; Windows: no RØDE app in the way, a drive letter for it). It only
// reads files and device information.
func Preflight(o Options) *Report {
	r := &Report{}
	if err := Supported(); err != nil {
		r.add(fail("system", err))
	}
	r.checkArchive(o.StageDir)
	if Supported() == nil {
		r.checkDevice(o.Node)
		r.hostChecks(o)
	}
	return r
}

// detect finds the devices for Preflight; tests replace it.
var detect = FindDevices

// checkDevice finds the device (or takes the one given), sets r.Node, and
// refuses anything that is not one RØDECaster Duo in its normal mode: the
// image and the procedure are the Duo's. A device given by node is taken as
// it is (the operator said which one).
func (r *Report) checkDevice(node string) {
	if node != "" {
		r.Node = node
		r.add(pass("device", node+" (given)"))
		return
	}
	devs, err := detect()
	switch {
	case err != nil:
		r.add(fail("device", err))
		return
	case len(devs) == 0:
		r.add(fail("device", ErrNoDevice))
		return
	}
	r.Devices = devs
	d := devs[0]
	for _, x := range devs {
		if x.Model() == "duo" {
			d = x
			break
		}
	}
	c := pass("device", d.String())
	switch d.Model() {
	case "duo":
		if d.InUpdateMode() {
			// 'M' would re-create the update disk under a run's feet.
			r.add(fail("device", fmt.Errorf("%s is in update mode already (an earlier run copied an archive onto it): switch the Duo off and on again, then check again", d)))
			return
		}
		c.Detail += ", a RØDECaster Duo"
	case "pro2":
		r.add(fail("device", fmt.Errorf("%s is a RØDECaster Pro II; rcplug flashes only the RØDECaster Duo", d)))
		return
	default:
		r.add(fail("device", fmt.Errorf("%s has a USB product id rcplug does not know for the RØDECaster Duo (it knows those of firmware 1.7.6); if it is a Duo, name it with --node", d)))
		return
	}
	duos := 0
	for _, x := range devs {
		if x.Model() == "duo" {
			duos++
		}
	}
	if duos > 1 {
		r.add(fail("device", fmt.Errorf("%d RØDECaster Duos found: connect only the one to flash", duos)))
		return
	}
	if len(devs) > 1 {
		c.Warn = true
		c.Detail += fmt.Sprintf("; %d other RØDE HID interface(s) found and left alone — better connect only the Duo", len(devs)-1)
	}
	r.Node = d.Node
	r.add(c)
}

// checkArchive checks the staged files and what the image is.
func (r *Report) checkArchive(dir string) {
	st, err := CheckStaging(dir)
	if err != nil {
		r.add(fail("archive", err))
		return
	}
	r.Staging = st
	if !st.MD5OK {
		r.add(fail("archive", fmt.Errorf("%s does not match %s (archive md5 %s, checksum file says %q); the device would reject it — re-run rcplug-cfw",
			stagedName, md5Name, st.Digest, st.Declared)))
		return
	}
	r.add(pass("archive", fmt.Sprintf("%s (%d MB, md5 %s, matches %s)", st.Archive, st.Size>>20, st.Digest, md5Name)))

	// The bundled scripts are for the Duo; refuse an image for anything else.
	in, err := fwimage.Inspect(st.Archive)
	if err != nil {
		r.add(fail("image", err))
		return
	}
	r.Image = in
	duo, _ := fwimage.ModelByID("duo")
	if err := in.Check(duo, ""); err != nil {
		r.add(fail("image", err))
		return
	}
	if in.Keys == 0 {
		r.add(Check{ID: "image", OK: true, Warn: true,
			Detail: in.Describe() + ", no SSH key in it: flashing it gives no SSH access (fine to go back to stock)"})
		return
	}
	r.add(pass("image", fmt.Sprintf("%s, %d SSH key(s)", in.Describe(), in.Keys)))
}

// Prepare runs Preflight and returns the plan for a run, or the first problem
// as an error. It sends nothing to the device.
func Prepare(o Options) (*Plan, error) {
	r := Preflight(o)
	if err := r.Err(); err != nil {
		return nil, err
	}
	p := &Plan{Staging: r.Staging, Image: r.Image, Python: r.Python, Node: r.Node}
	for _, d := range r.Devices {
		if d.Node == r.Node {
			p.Device = d
		}
	}
	for _, c := range r.Checks {
		if c.Warn {
			p.Warnings = append(p.Warnings, c.ID+": "+c.Detail)
		}
	}
	return p, nil
}
