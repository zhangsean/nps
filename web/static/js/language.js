(function ($) {

	function xml2json(Xml) {
		var tempvalue, tempJson = {};
		$(Xml).each(function() {
			var tagName = ($(this).attr('id') || this.tagName);
			tempvalue = (this.childElementCount == 0) ? this.textContent : xml2json($(this).children());
			switch ($.type(tempJson[tagName])) {
				case 'undefined':
					tempJson[tagName] = tempvalue;
					break;
				case 'object':
					tempJson[tagName] = Array(tempJson[tagName]);
				case 'array':
					tempJson[tagName].push(tempvalue);
			}
		});
		return tempJson;
	}

	function setCookie (c_name, value, expiredays) {
		var exdate = new Date();
		exdate.setDate(exdate.getDate() + expiredays);
		document.cookie = c_name + '=' + escape(value) + ((expiredays == null) ? '' : ';expires=' + exdate.toGMTString())+ '; path='+window.nps.web_base_url+'/;';
	}

	function getCookie (c_name) {
		if (document.cookie.length > 0) {
			c_start = document.cookie.indexOf(c_name + '=');
			if (c_start != -1) {
				c_start = c_start + c_name.length + 1;
				c_end = document.cookie.indexOf(';', c_start);
				if (c_end == -1) c_end = document.cookie.length;
				return unescape(document.cookie.substring(c_start, c_end));
			}
		}
		return null;
	}

	function setchartlang (langobj,chartobj) {
		if ( $.type (langobj) == 'string' ) return langobj;
		if ( $.type (langobj) == 'chartobj' ) return false;
		var flag = true;
		for (key in langobj) {
			var item = key;
			children = (chartobj.hasOwnProperty(item)) ? setchartlang (langobj[item],chartobj[item]) : setchartlang (langobj[item],undefined);
			switch ($.type(children)) {
				case 'string':
					if ($.type(chartobj[item]) != 'string' ) continue;
				case 'object':
					chartobj[item] = (children['value'] || children);
				default:
					flag = false;
			}
		}
		if (flag) { return {'value':(langobj[languages['current']] || langobj[languages['default']] || 'N/A')}}
	}

	$.fn.cloudLang = function () {
		$.ajax({
			type: 'GET',
			url: window.nps.web_base_url + '/static/page/languages.xml?v=20240528',
			dataType: 'xml',
			success: function (xml) {
				languages['content'] = xml2json($(xml).children())['content'];
				languages['menu'] = languages['content']['languages'];
				languages['default'] = languages['content']['default'];
				languages['navigator'] = (getCookie ('lang') || navigator.language || navigator.browserLanguage);
				for(var key in languages['menu']){
					$('#languagemenu').next().append('<li lang="' + key + '"><a><img src="' + window.nps.web_base_url + '/static/img/flag/' + key + '.png"> ' + languages['menu'][key] +'</a></li>');
					if ( key == languages['navigator'] ) languages['current'] = key;
				}
				$('#languagemenu').attr('lang',(languages['current'] || languages['default']));
				$('body').setLang ('');
			}
		});
	};

	$.fn.setLang = function (dom) {
		languages['current'] = $('#languagemenu').attr('lang');
		if ( dom == '' ) {
			$('#languagemenu span').text(' ' + languages['menu'][languages['current']]);
			if (languages['current'] != getCookie('lang')) setCookie('lang', languages['current']);
			if($("#table").length>0) {
				var tableOptions = $('#table').bootstrapTable('getOptions') || {};
				$('#table').bootstrapTable('refreshOptions', {
					'locale': languages['current'],
					'pageNumber': tableOptions.pageNumber || 1,
					'pageSize': tableOptions.pageSize
				});
			}
		}
		$.each($(dom + ' [langtag]'), function (i, item) {
			var index = $(item).attr('langtag');
			string = languages['content'][index.toLowerCase()];
			switch ($.type(string)) {
				case 'string':
					break;
				case 'array':
					string = string[Math.floor((Math.random()*string.length))];
				case 'object':
					string = (string[languages['current']] || string[languages['default']] || null);
					break;
				default:
					string = 'Missing language string "' + index + '"';
					$(item).css('background-color','#ffeeba');
			}
			if($.type($(item).attr('placeholder')) == 'undefined') {
				$(item).text(string);
			} else {
				$(item).attr('placeholder', string);
			}
		});

		if ( !$.isEmptyObject(chartdatas) ) {
			setchartlang(languages['content']['charts'],chartdatas);
			for(var key in chartdatas){
				if ($('#'+key).length == 0) continue;
				if($.type(chartdatas[key]) == 'object')
				charts[key] = echarts.init(document.getElementById(key));
				charts[key].setOption(chartdatas[key], true);
			}
		}
	}

})(jQuery);

$(document).ready(function () {
	$('body').cloudLang();
	$('body').on('click','li[lang]',function(){
		$('#languagemenu').attr('lang',$(this).attr('lang'));
		$('body').setLang ('');
	});
});

var languages = {};
var charts = {};
var chartdatas = {};
var postsubmit;
var npsNotifyTimer;

function npsLangValue(key, fallback) {
    var langobj = languages['content'] && languages['content'][key];
    if ($.type(langobj) == 'undefined') return fallback;
    if ($.type(langobj) == 'string') return langobj;
    return langobj[languages['current']] || langobj[languages['default']] || fallback;
}

function langreply(langstr) {
    var langobj = languages['content']['reply'][langstr.replace(/[\s,\.\?]*/g,"").toLowerCase()];
    if ($.type(langobj) == 'undefined') return langstr
    langobj = (langobj[languages['current']] || langobj[languages['default']] || langstr);
    return langobj
}

function npsNotify(message, status) {
    var box = $("#nps-notify");
    if (!box.length) {
        box = $('<div id="nps-notify" role="status" aria-live="polite"></div>');
        $("body").append(box);
    }
    clearTimeout(npsNotifyTimer);
    box.removeClass("is-success is-error is-show");
    box.addClass(status ? "is-success" : "is-error");
    box.text(message || "");
    setTimeout(function () {
        box.addClass("is-show");
    }, 10);
    npsNotifyTimer = setTimeout(function () {
        box.removeClass("is-show");
    }, 3000);
}

function npsConfirm(message, onConfirm) {
    var confirmBox = $("#nps-confirm");
    if (!confirmBox.length) {
        confirmBox = $(
            '<div id="nps-confirm" class="nps-confirm-mask" aria-hidden="true">' +
                '<div class="nps-confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="nps-confirm-title" aria-describedby="nps-confirm-message" tabindex="-1">' +
                    '<div class="nps-confirm-head">' +
                        '<span class="nps-confirm-icon"><i class="fa fa-exclamation-circle"></i></span>' +
                        '<div id="nps-confirm-title" class="nps-confirm-title"></div>' +
                    '</div>' +
                    '<div id="nps-confirm-message" class="nps-confirm-message"></div>' +
                    '<div class="nps-confirm-actions">' +
                        '<button type="button" class="btn btn-white nps-confirm-cancel"></button>' +
                        '<button type="button" class="btn btn-primary nps-confirm-ok"></button>' +
                    '</div>' +
                '</div>' +
            '</div>'
        );
        $("body").append(confirmBox);

        confirmBox.on("click", function (event) {
            if (event.target === this) {
                npsCloseConfirm();
            }
        });
        confirmBox.on("click", ".nps-confirm-cancel", function () {
            npsCloseConfirm();
        });
        confirmBox.on("click", ".nps-confirm-ok", function () {
            var callback = confirmBox.data("onConfirm");
            npsCloseConfirm();
            if ($.isFunction(callback)) callback();
        });
        $(document).on("keydown.npsConfirm", function (event) {
            if ((event.key === "Escape" || event.keyCode === 27) && confirmBox.hasClass("is-show")) {
                npsCloseConfirm();
            }
        });
    }

    var isZh = (languages['current'] || '').indexOf('zh') === 0;
    confirmBox.find(".nps-confirm-title").text(isZh ? "操作确认" : "Confirm action");
    confirmBox.find(".nps-confirm-message").text(message || "");
    confirmBox.find(".nps-confirm-cancel").text(npsLangValue("word-cancel", isZh ? "取消" : "Cancel"));
    confirmBox.find(".nps-confirm-ok").text(isZh ? "确定" : "OK");
    confirmBox.data("onConfirm", onConfirm);
    confirmBox.attr("aria-hidden", "false").addClass("is-show");
    confirmBox.find(".nps-confirm-cancel").trigger("focus");
}

function npsCloseConfirm() {
    var confirmBox = $("#nps-confirm");
    confirmBox.removeClass("is-show").attr("aria-hidden", "true").removeData("onConfirm");
}

function npsRefreshTable(selector) {
    var table = $(selector || "#table");
    var options;
    if (!table.length || !$.isFunction(table.bootstrapTable)) {
        document.location.reload();
        return;
    }
    try {
        options = table.bootstrapTable("getOptions") || {};
        table.one("load-success.bs.table load-error.bs.table", function () {
            var nextOptions = table.bootstrapTable("getOptions") || {};
            if (options.pageNumber > 1 && nextOptions.pageNumber !== options.pageNumber) {
                table.bootstrapTable("selectPage", options.pageNumber);
            }
        });
        table.bootstrapTable("refresh", {
            silent: true,
            pageNumber: options.pageNumber || 1,
            pageSize: options.pageSize,
            sortName: options.sortName,
            sortOrder: options.sortOrder
        });
    } catch (e) {
        document.location.reload();
    }
}

function goback() {
	history.back();
}

function submitform(action, url, postdata) {
    postsubmit = false;
	$.each(postdata, function(i, v){
		if (v['value']) {
			v['value'] = v['value'].trim();
		}
	});
    function submitAction(reloadAfterSuccess) {
        postsubmit = reloadAfterSuccess;
        $.ajax({
            type: "POST",
            url: url,
            data: postdata,
            success: function (res) {
                npsNotify(langreply(res.msg), res.status);
                if (res.status) {
                    if (postsubmit) {npsRefreshTable();}else{history.back(-1);}
                }
            }
        });
    }
    switch (action) {
        case 'start':
        case 'stop':
        case 'delete':
            var langobj = languages['content']['confirm'][action];
            var message = (langobj[languages['current']] || langobj[languages['default']] || 'Are you sure you want to ' + action + ' it?');
            npsConfirm(message, function () {
                submitAction(true);
            });
            return;
        case 'add':
        case 'edit':
            submitAction(false);
			return;
		case 'global':
			$.ajax({
				type: "POST",
				url: url,
				data: postdata,
				success: function (res) {
					npsNotify(langreply(res.msg), res.status);
					if (res.status) {
						npsRefreshTable();
					}
				}
			});
    }
}

function changeunit(limit) {
    var size = "";
    if (limit < 0.1 * 1024) {
        size = limit.toFixed(2) + "B";
    } else if (limit < 0.1 * 1024 * 1024) {
        size = (limit / 1024).toFixed(2) + "KB";
    } else if (limit < 0.1 * 1024 * 1024 * 1024) {
        size = (limit / (1024 * 1024)).toFixed(2) + "MB";
    } else {
        size = (limit / (1024 * 1024 * 1024)).toFixed(2) + "GB";
    }

    var sizeStr = size + "";
    var index = sizeStr.indexOf(".");
    var dou = sizeStr.substr(index + 1, 2);
    if (dou == "00") {
        return sizeStr.substring(0, index) + sizeStr.substr(index + 3, 2);
    }
    return size;
}

function getOfflineStatus(lastConnTime) {
	if (lastConnTime === 0) {
		return '<span class="badge badge-badge" langtag="word-not-connected"></span>'
	}
	let offSeconds = parseInt(Date.parse(new Date())/1000 - lastConnTime);
	let timeStr = '';
	let color = '';

	if (offSeconds < 60) {
		timeStr = offSeconds + ' <span langtag="word-second"></span>';
		color = '#28a745';
	} else if (offSeconds < 3600) {
		timeStr = parseInt(offSeconds / 60) + ' <span langtag="word-minute"></span>';
		color = offSeconds >= 600 ? 'red' : '#ffc107';
	} else if (offSeconds < 86400) {
		timeStr = parseInt(offSeconds / 3600) + ' <span langtag="word-hour"></span>';
		color = 'red';
	} else {
		timeStr = parseInt(offSeconds / 86400) + ' <span langtag="word-day"></span>';
		color = 'red';
	}

	return '<span class="badge badge-badge" langtag="word-offline"></span><span style="color:' + color + '" title="掉线时间：' + timeString(lastConnTime) + '"> ' + timeStr + '</span>'
}

function timeString(time) {
    let dt = new Date(time * 1000);
    return dt.getFullYear() + "-" + intFmt(dt.getMonth() + 1) + "-" + intFmt(dt.getDate()) + " " +
		intFmt(dt.getHours()) + ":" + intFmt(dt.getMinutes()) + ":" + intFmt(dt.getSeconds());
}

function intFmt(n, len) {
	len = len || 2;
	let str = n + '';
	for (let i = str.length; i < len; i++) {
		str = '0' + str;
	}
	return str
}
