package bot

var i18n = map[string]map[string]string{
	"en": {
		"captcha_prompt":        "👋 Welcome! Please verify you are human by tapping the button below within 60 seconds.",
		"captcha_button":        "✅ I am human",
		"captcha_verified":      "✅ Verification successful. Welcome!",
		"captcha_expired":       "⏰ Verification expired. You have been removed.",
		"strike1":               "⚠️ Warning: your message violated group rules. This is strike 1.",
		"strike2":               "🔇 You have been muted for 2 hours due to repeated violations.",
		"strike3":               "🚫 You have been permanently removed for repeated violations.",
		"chat_locked":           "🔒 The chat has been locked by the group owner.",
		"chat_unlocked":         "🔓 The chat has been unlocked.",
		"dashboard_title":       "⚙️ Group Settings",
		"daily_report":          "📊 Daily Moderation Report",
		"msg_deleted":           "Messages deleted",
		"spammers_kicked":       "Spammers kicked",
		"strikes_issued":        "Strikes issued",
		"settings_usage":        "Usage: /settings <group_chat_id>",
		"not_group_admin":       "⛔ You are not an admin of that group.",
		"not_group_owner":       "⛔ Only the Telegram group/channel owner can change these settings.",
		"not_bot_owner":         "⛔ This command is only for the bot owner configured in BOT_OWNER_IDS.",
		"dashboard_saved":       "✅ Settings saved.",
		"dashboard_save_failed": "❌ Could not save settings. Please try again.",
		"badword_usage":         "Usage: /badword add <word>, /badword remove <word>, or /badword list",
		"allow_usage":           "Usage: /allowdomain add <domain>, /allowdomain remove <domain>, or /allowdomain list",
		"log_channel_set":       "✅ This chat is now the moderation log channel.",
		"log_channel_cleared":   "✅ Moderation log channel cleared.",
		"unknown_admin_command": "Unknown admin command.",
		"report_usage":          "Reply to a message with /report to report it to admins.",
		"report_no_log_channel": "No moderation log channel is configured. Ask an admin to run /setlog first.",
		"report_no_user":        "That message cannot be reported because no sender is attached.",
		"report_failed":         "❌ Could not send the report to admins.",
		"report_sent":           "✅ Report sent to admins.",
	},
	"km": {
		"captcha_prompt":        "👋 សូមស្វាគមន៍! សូមផ្ទៀងផ្ទាត់ថាអ្នកជាមនុស្សដោយចុចប៊ូតុងខាងក្រោមក្នុងរយៈពេល ៦០ វិនាទី។",
		"captcha_button":        "✅ ខ្ញុំជាមនុស្ស",
		"captcha_verified":      "✅ ការផ្ទៀងផ្ទាត់ជោគជ័យ។ សូមស្វាគមន៍!",
		"captcha_expired":       "⏰ ការផ្ទៀងផ្ទាត់ផុតកំណត់។ អ្នកត្រូវបានដកចេញ។",
		"strike1":               "⚠️ ព្រមាន: សាររបស់អ្នកបានបំពានច្បាប់ក្រុម។ នេះជាការវាយប្រហារទី ១។",
		"strike2":               "🔇 អ្នកត្រូវបានបិទសិទ្ធិសរសេររយៈពេល ២ ម៉ោង ដោយសារការបំពានម្ដងហើយម្ដងទៀត។",
		"strike3":               "🚫 អ្នកត្រូវបានដកចេញជាអចិន្ត្រៃយ៍ ដោយសារការបំពានម្ដងហើយម្ដងទៀត។",
		"chat_locked":           "🔒 ការជជែករបស់ក្រុមត្រូវបានចាក់សោដោយម្ចាស់ក្រុម។",
		"chat_unlocked":         "🔓 ការជជែករបស់ក្រុមត្រូវបានដោះសោ។",
		"dashboard_title":       "⚙️ ការកំណត់ក្រុម",
		"daily_report":          "📊 របាយការណ៍ប្រចាំថ្ងៃ",
		"msg_deleted":           "សារដែលបានលុប",
		"spammers_kicked":       "អ្នកបំពានត្រូវបានដកចេញ",
		"strikes_issued":        "ការវាយប្រហារបានចេញ",
		"settings_usage":        "របៀបប្រើ: /settings <group_chat_id>",
		"not_group_admin":       "⛔ អ្នកមិនមែនជាអ្នកគ្រប់គ្រងក្រុមនោះទេ។",
		"not_group_owner":       "⛔ មានតែម្ចាស់ group/channel នៅ Telegram ប៉ុណ្ណោះដែលអាចកែការកំណត់នេះបាន។",
		"not_bot_owner":         "⛔ ពាក្យបញ្ជានេះសម្រាប់ម្ចាស់ bot ដែលកំណត់ក្នុង BOT_OWNER_IDS ប៉ុណ្ណោះ។",
		"dashboard_saved":       "✅ បានរក្សាទុកការកំណត់។",
		"dashboard_save_failed": "❌ មិនអាចរក្សាទុកការកំណត់បានទេ។ សូមព្យាយាមម្ដងទៀត។",
		"badword_usage":         "របៀបប្រើ: /badword add <word>, /badword remove <word>, ឬ /badword list",
		"allow_usage":           "របៀបប្រើ: /allowdomain add <domain>, /allowdomain remove <domain>, ឬ /allowdomain list",
		"log_channel_set":       "✅ ក្រុមនេះត្រូវបានកំណត់ជាបន្ទប់កំណត់ត្រា moderation។",
		"log_channel_cleared":   "✅ បានលុបបន្ទប់កំណត់ត្រា moderation។",
		"unknown_admin_command": "ពាក្យបញ្ជាអ្នកគ្រប់គ្រងមិនស្គាល់។",
		"report_usage":          "សូម reply ទៅសារមួយ រួចប្រើ /report ដើម្បីរាយការណ៍ទៅអ្នកគ្រប់គ្រង។",
		"report_no_log_channel": "មិនទាន់បានកំណត់បន្ទប់ moderation log ទេ។ សូមឲ្យ admin ប្រើ /setlog ជាមុន។",
		"report_no_user":        "មិនអាចរាយការណ៍សារនេះបានទេ ព្រោះមិនមានអ្នកផ្ញើ។",
		"report_failed":         "❌ មិនអាចផ្ញើ report ទៅ admin បានទេ។",
		"report_sent":           "✅ បានផ្ញើ report ទៅ admin។",
	},
}

func T(lang, key string) string {
	if m, ok := i18n[lang]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := i18n["en"][key]; ok {
		return s
	}
	return key
}

func supportedLanguage(lang string) bool {
	switch lang {
	case "en", "km":
		return true
	default:
		return false
	}
}
