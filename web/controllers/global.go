package controllers

type GlobalController struct {
	BaseController
}

func (s *GlobalController) Index() {
	s.requireConfigAdmin(false)
	s.Ctx.Output.Header("Cache-Control", "no-store")
	if err := s.loadRuntimeConfigData(); err != nil {
		s.Ctx.Output.SetStatus(500)
		s.Ctx.WriteString("cannot read nps.conf: " + err.Error())
		return
	}
	s.Data["menu"] = "global"
	s.Data["bodyClass"] = "runtime-config-page"
	s.SetInfo("global")
	s.display("global/index")
}
