(function ($) {
    function intValue(val, def) {
        var n = parseInt(val, 10);
        return isNaN(n) ? def : n;
    }

    function PortSelector(opts) {
        this.taskId = opts.taskId || 0;
        this.webBaseUrl = opts.webBaseUrl || "";
        this.$modal = $("#nps-port-selector-modal");
        this.$type = $("#type");
        this.$port = $("input[name='port']");
        this.$remark = $("input[name='remark']");
        this.$target = $("textarea[name='target']");
        this.$open = $("#open-port-selector");
        this.$segment = $("#port-range-segment");
        this.$start = $("#port-range-start");
        this.$pageSize = $("#port-page-size");
        this.$summary = $("#port-range-summary");
        this.$tbody = $("#port-range-rows");
        this.$empty = $("#port-range-empty");
        this.$useStart = $("#use-start-port");
        this.$load = $("#load-port-range");
        this.$prev = $("#prev-port-range");
        this.$next = $("#next-port-range");
        this.state = {
            mode: "tcp",
            data: null,
            rangeStart: 1,
            rangeEnd: 200,
            firstUsable: 0
        };
    }

    PortSelector.prototype.init = function () {
        if (!this.$modal.length || !this.$type.length || !this.$port.length || !this.$open.length) {
            return;
        }
        this.bindEvents();
        this.syncModeButton();
    };

    PortSelector.prototype.bindEvents = function () {
        var self = this;
        this.$type.on("change", function () {
            self.syncModeButton();
        });
        this.$open.on("click", function () {
            self.open();
        });
        this.$segment.on("change", function () {
            var segment = self.getCurrentSegment();
            if (!segment) {
                return;
            }
            self.$start.val(segment.start);
            self.load();
        });
        this.$load.on("click", function () {
            self.load();
        });
        this.$prev.on("click", function () {
            self.changePage(-1);
        });
        this.$next.on("click", function () {
            self.changePage(1);
        });
        this.$useStart.on("click", function () {
            self.usePort(self.state.firstUsable);
        });
        this.$tbody.on("click", ".js-use-port", function () {
            self.usePort(intValue($(this).data("port"), 0));
        });
    };

    PortSelector.prototype.syncModeButton = function () {
        var mode = this.$type.val();
        var enabled = mode === "tcp" || mode === "udp";
        this.$open.toggle(enabled);
        if (enabled) {
            this.$open.find(".js-proto-name").text(mode.toUpperCase());
        }
    };

    PortSelector.prototype.open = function () {
        var mode = this.$type.val();
        if (mode !== "tcp" && mode !== "udp") {
            alert("仅 TCP/UDP 隧道支持端口选择器");
            return;
        }
        this.state.mode = mode;
        this.$modal.find(".js-current-mode").text(mode.toUpperCase());
        this.load(true);
        this.$modal.modal("show");
    };

    PortSelector.prototype.load = function (resetStart) {
        var self = this;
        var pageSize = intValue(this.$pageSize.val(), 200);
        var startPort = intValue(this.$start.val(), 0);

        if (resetStart || startPort <= 0) {
            startPort = this.getInitialStart();
            this.$start.val(startPort);
        }

        $.ajax({
            type: "GET",
            url: this.webBaseUrl + "/index/portlist",
            data: {
                mode: this.state.mode,
                start: startPort,
                end: startPort + pageSize - 1,
                id: this.taskId
            },
            success: function (res) {
                if (!res.status) {
                    alert(langreply(res.msg || "加载端口列表失败"));
                    return;
                }
                self.state.data = res.data;
                self.state.rangeStart = res.data.range_start;
                self.state.rangeEnd = res.data.range_end;
                self.state.firstUsable = intValue(res.data.first_usable, 0);
                self.renderSegments();
                self.renderTable();
            }
        });
    };

    PortSelector.prototype.getInitialStart = function () {
        var currentValue = intValue(this.$port.val(), 0);
        var ranges = this.state.data ? this.state.data.ranges : [];
        var i;
        for (i = 0; i < ranges.length; i++) {
            if (currentValue >= ranges[i].start && currentValue <= ranges[i].end) {
                return currentValue;
            }
        }
        if (ranges.length > 0) {
            return ranges[0].start;
        }
        return 1;
    };

    PortSelector.prototype.getCurrentSegment = function () {
        var data = this.state.data;
        if (!data || !data.ranges || !data.ranges.length) {
            return null;
        }
        var index = intValue(this.$segment.val(), 0);
        if (index < 0 || index >= data.ranges.length) {
            index = 0;
        }
        return data.ranges[index];
    };

    PortSelector.prototype.renderSegments = function () {
        var ranges = this.state.data.ranges || [];
        var selectedIndex = 0;
        var i;
        for (i = 0; i < ranges.length; i++) {
            if (this.state.rangeStart >= ranges[i].start && this.state.rangeStart <= ranges[i].end) {
                selectedIndex = i;
                break;
            }
        }

        this.$segment.empty();
        for (i = 0; i < ranges.length; i++) {
            this.$segment.append($("<option></option>").val(i).text(ranges[i].label));
        }
        this.$segment.val(selectedIndex);
        this.$start.val(this.state.rangeStart);
    };

    PortSelector.prototype.renderTable = function () {
        var rows = this.state.data.rows || [];
        var blocked = {};
        var i;

        for (i = 0; i < rows.length; i++) {
            if (!rows[i].is_current) {
                blocked[rows[i].port] = true;
            }
        }

        this.$tbody.empty();
        this.$summary.text("当前范围: " + this.state.rangeStart + " - " + this.state.rangeEnd + "，占用端口 " + rows.length + " 个");

        if (!rows.length) {
            this.$empty.text("当前范围内没有占用端口。").show();
        } else {
            this.$empty.hide();
        }

        for (i = 0; i < rows.length; i++) {
            this.$tbody.append(this.renderRow(rows[i], blocked));
        }

        this.renderFirstUsable();
        this.updatePager();
    };

    PortSelector.prototype.renderFirstUsable = function () {
        if (this.state.firstUsable > 0) {
            this.$useStart.text("使用 " + this.state.firstUsable).prop("disabled", false).show();
            return;
        }
        this.$useStart.text("当前页无可用端口").prop("disabled", true).show();
    };

    PortSelector.prototype.renderRow = function (row, blocked) {
        var $tr = $("<tr></tr>");
        var $portCell = $("<td class='port-occupied'></td>").text(row.port);
        var $ownerCell = $("<td></td>");
        var $actionCell = $("<td class='text-right'></td>");
        var actions = this.getQuickActions(row, blocked);
        var tooltip = this.getRowTooltip(row);
        var i;

        if (row.is_current) {
            $portCell.removeClass("port-occupied").addClass("port-current");
        }

        $ownerCell.append(
            $("<span class='port-owner'></span>").attr("title", tooltip).text(row.owner || "系统进程")
        );

        if (!actions.length) {
            $actionCell.append($("<span class='text-muted'>无</span>"));
        } else {
            for (i = 0; i < actions.length; i++) {
                $actionCell.append(
                    $("<button type='button' class='btn btn-outline btn-sm btn-primary m-r-xs js-use-port'></button>")
                        .attr("data-port", actions[i])
                        .text(actions[i])
                );
            }
        }

        $tr.append($portCell).append($ownerCell).append($actionCell);
        return $tr;
    };

    PortSelector.prototype.getRowTooltip = function (row) {
        var lines = [];
        if (row.tooltip) {
            lines.push(row.tooltip);
        }
        if (row.is_current) {
            if (this.$remark.val()) {
                lines.push("隧道备注: " + this.$remark.val());
            }
            if (this.$target.val()) {
                lines.push("隧道目标: " + this.$target.val());
            }
        }
        return lines.join("\n");
    };

    PortSelector.prototype.getQuickActions = function (row, blocked) {
        var before = [];
        var after = [];
        var port;

        for (port = row.port - 1; port >= this.state.rangeStart && row.port - port <= 10 && before.length < 5; port--) {
            if (this.canUsePort(port, blocked)) {
                before.push(port);
            }
        }

        before.reverse();

        for (port = row.port + 1; port <= this.state.rangeEnd && port - row.port <= 10 && after.length < 5; port++) {
            if (this.canUsePort(port, blocked)) {
                after.push(port);
            }
        }

        return before.concat(after);
    };

    PortSelector.prototype.canUsePort = function (port, blocked) {
        return port >= this.state.rangeStart &&
            port <= this.state.rangeEnd &&
            !blocked[port];
    };

    PortSelector.prototype.changePage = function (direction) {
        var segment = this.getCurrentSegment();
        var pageSize = intValue(this.$pageSize.val(), 200);
        var nextStart;
        if (!segment) {
            return;
        }
        if (direction < 0) {
            nextStart = this.state.rangeStart - pageSize;
            if (nextStart < segment.start) {
                nextStart = segment.start;
            }
        } else {
            nextStart = this.state.rangeEnd + 1;
            if (nextStart > segment.end) {
                return;
            }
        }
        this.$start.val(nextStart);
        this.load();
    };

    PortSelector.prototype.updatePager = function () {
        var segment = this.getCurrentSegment();
        if (!segment) {
            this.$prev.prop("disabled", true);
            this.$next.prop("disabled", true);
            return;
        }
        this.$prev.prop("disabled", this.state.rangeStart <= segment.start);
        this.$next.prop("disabled", this.state.rangeEnd >= segment.end);
    };

    PortSelector.prototype.usePort = function (port) {
        if (!port) {
            return;
        }
        this.$port.val(port);
        this.$modal.modal("hide");
    };

    window.initPortSelector = function (opts) {
        var selector = new PortSelector(opts || {});
        selector.init();
        return selector;
    };
})(jQuery);
