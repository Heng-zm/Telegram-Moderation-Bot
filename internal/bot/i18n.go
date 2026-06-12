package bot

// i18n holds localised bot messages.
var i18n = map[string]map[string]string{
	"en": {
		"captcha_prompt":    "👋 Welcome! Please verify you are human by tapping the button below within 60 seconds.",
		"captcha_button":    "✅ I am human",
		"captcha_verified":  "✅ Verification successful. Welcome!",
		"captcha_expired":   "⏰ Verification expired. You have been removed.",
		"strike1":           "⚠️ Warning: your message violated group rules. This is strike 1.",
		"strike2":           "🔇 You have been muted for 2 hours due to repeated violations.",
		"strike3":           "🚫 You have been permanently removed for repeated violations.",
		"chat_locked":       "🔒 The chat has been locked by an admin.",
		"chat_unlocked":     "🔓 The chat has been unlocked.",
		"dashboard_title":   "⚙️ Group Settings",
		"daily_report":      "📊 Daily Moderation Report",
		"msg_deleted":       "Messages deleted",
		"spammers_kicked":   "Spammers kicked",
		"strikes_issued":    "Strikes issued",
	},
	"km": {
		"captcha_prompt":    "👋 សូមស្វាគមន៍! សូមផ្ទៀងផ្ទាត់ថាអ្នកជាមនុស្សដោយចុចប៊ូតុងខាងក្រោមក្នុងរយៈពេល ៦០ វិនាទី។",
		"captcha_button":    "✅ ខ្ញុំជាមនុស្ស",
		"captcha_verified":  "✅ ការផ្ទៀងផ្ទាត់ជោគជ័យ។ សូមស្វាគមន៍!",
		"captcha_expired":   "⏰ ការផ្ទៀងផ្ទាត់ផុតកំណត់។ អ្នកត្រូវបានដកចេញ។",
		"strike1":           "⚠️ ព្រមាន: សាររបស់អ្នកបានបំពានច្បាប់ក្រុម។ នេះជាការវាយប្រហារទី ១។",
		"strike2":           "🔇 អ្នកត្រូវបានបិទសិទ្ធិសរសេររយៈពេល ២ ម៉ោង ដោយសារការបំពានម្ដងហើយម្ដងទៀត។",
		"strike3":           "🚫 អ្នកត្រូវបានដកចេញជាអចិន្ត្រៃយ៍ ដោយសារការបំពានម្ដងហើយម្ដងទៀត។",
		"chat_locked":       "🔒 ការជជែករបស់ក្រុមត្រូវបានចាក់សោដោយអ្នកគ្រប់គ្រង។",
		"chat_unlocked":     "🔓 ការជជែករបស់ក្រុមត្រូវបានដោះសោ។",
		"dashboard_title":   "⚙️ ការកំណត់ក្រុម",
		"daily_report":      "📊 របាយការណ៍ប្រចាំថ្ងៃ",
		"msg_deleted":       "សារដែលបានលុប",
		"spammers_kicked":   "អ្នកបំពានត្រូវបានដកចេញ",
		"strikes_issued":    "ការវាយប្រហារបានចេញ",
	},
}

// T returns a localised string for the given key and language code.
// Falls back to English if the language or key is not found.
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
