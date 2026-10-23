package hours

import (
	"html/template"
	"net/http"
)

var homeTemplate = template.Must(template.New("home").Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Планерум</title><link rel="stylesheet" href="/static/giga-tokens.css"><link rel="stylesheet" href="/static/app.css"></head>
<body><div class="gc-scope" data-embed="{{.Embed}}">
<header class="topbar">{{if not .Embed}}<img src="/static/logo-gigacowork.svg" alt="GigaCowork">{{end}}<div><h1>Планерум</h1><p>Создание и проверка образовательной программы</p></div></header>
{{if .Demo}}<div class="demo-warning" role="note"><strong>Демо-режим.</strong> Условия извлекаются офлайн, материалы создаются из тестовых шаблонов, данные хранятся только до перезапуска сервера.</div>{{end}}
<div class="workspace">
<main class="panel canvas" aria-labelledby="canvas-title"><div class="request-head"><div><h2>Новый запрос</h2><p class="muted">Опишите курс своими словами. Мы сохраним уточнения в программе.</p></div><button id="new-request" class="secondary" type="button">Новый запрос</button></div>
<label for="message">Опишите курс</label><textarea id="message" maxlength="4000" aria-describedby="message-count" placeholder="Например: нужна программа повышения квалификации по анализу данных для начинающих, 72 часа, очно-заочно. Итоговая аттестация — защита проекта."></textarea>
<div class="send-row"><small id="message-count">0 / 4000</small><button id="send" class="primary" type="button">Отправить</button></div>
<div id="dialogue" class="dialogue" role="log" aria-live="polite" aria-label="История обсуждения"></div>
<div class="canvas-head"><div><div class="eyebrow">Рабочая область</div><h2 id="canvas-title">Программа еще не создана</h2></div><span id="revision" class="pill">Новый запрос</span></div>
<p id="state" class="muted" role="status">Опишите курс или заполните параметры.</p>
<div id="questions" class="notice" aria-live="polite" hidden></div>
<div id="overview" class="overview"><div class="empty">Каркас, результаты обучения и план появятся здесь.</div></div>
<section id="editor" class="block" hidden><h3>Точечная правка</h3><p class="muted">Изменение цели или темы потребует нового утверждения. Правка оценочного материала сохранит утвержденный каркас.</p><label for="edit-target">Часть программы</label><select id="edit-target"></select><label for="edit-value">Новый текст</label><textarea id="edit-value"></textarea><button id="edit-apply" class="secondary" type="button">Сохранить новую версию</button></section>
<details id="standard-panel" class="block" hidden><summary>Привязка к профстандарту</summary>
<p class="muted">Укажите проверенный источник и точный код трудовой функции. Изменение привязки снимает утверждение и удаляет подготовленные материалы: каркас потребуется утвердить заново.</p>
<label for="standard-outcome">Результат обучения</label><select id="standard-outcome"></select>
<label for="standard-code">Код трудовой функции</label><input id="standard-code" maxlength="64" placeholder="Код из документа">
<label for="standard-title">Название профстандарта</label><input id="standard-title" maxlength="150">
<label for="standard-location">Ссылка или имя загруженного документа</label><input id="standard-location" maxlength="250">
<label for="standard-evidence">Проверенный фрагмент с точным кодом</label><textarea id="standard-evidence" maxlength="4000"></textarea>
<label class="check"><input id="standard-confirmed" type="checkbox"> Я проверил источник и соответствие кода трудовой функции</label>
<button id="bind-standard" class="secondary" type="button" disabled>Связать и пересмотреть каркас</button></details>
<details id="raw" hidden><summary>Данные для интеграции · JSON</summary><pre id="result"></pre></details>
</main>
<aside class="panel parameters" aria-labelledby="parameters-title"><h2 id="parameters-title">Параметры</h2>
<p class="muted">Проверьте извлеченные условия и дополните их перед подготовкой каркаса.</p>
<div class="token-field"><label for="token">Токен доступа</label><input id="token" type="password" autocomplete="off" placeholder="Для защищенного сервера"></div>
<details id="parameters-details"><summary>Условия курса · редактировать</summary>
<div class="field-pair"><div><label for="kind">Тип программы</label><select id="kind"><option value="">Не указан</option><option value="qualification">Повышение квалификации</option><option value="general">Общеразвивающая</option></select></div><div><label for="requested">Объем, ч</label><input id="requested" type="number" min="16" max="500" step="0.5" placeholder="Не указан"></div></div>
<label for="title">Название</label><input id="title" maxlength="500" placeholder="Не указано">
<label for="audience">Аудитория</label><input id="audience" maxlength="500" placeholder="Не указана">
<label for="level">Входной уровень</label><input id="level" maxlength="500" placeholder="Не указан">
<label for="delivery">Форма обучения</label><input id="delivery" maxlength="500" placeholder="Не указана">
<label for="attestation">Итоговая аттестация</label><input id="attestation" maxlength="500" placeholder="Не указана">
<label class="check"><input id="internship" type="checkbox"> Самостоятельная стажировка</label>
<label for="requirements">Требования и референсы</label><textarea id="requirements" maxlength="64000" placeholder="Темы, цели, ограничения, документы"></textarea>
<label for="reference">Файл DOCX, PDF или TXT</label><input id="reference" type="file" accept=".docx,.pdf,.txt">
<button id="read-reference" class="secondary" type="button">Добавить текст файла</button>
<p class="muted">Текст файла добавляется к требованиям и отправляется вместе с условиями.</p></details>
<button id="create" class="primary wide" type="button">Подготовить каркас</button><p class="muted draft-warning">Каркас создается только после проверки параметров и нажатия этой кнопки.</p>
</aside>
<aside class="panel review" aria-labelledby="review-title"><h2 id="review-title">Проверка и действия</h2>
<ol id="steps" class="steps"><li>Требования</li><li>Каркас</li><li>Утверждение</li><li>Материалы</li><li>Экспорт</li></ol>
<div id="regulatory" class="summary" hidden></div>
<div id="hours" class="summary"><strong>Баланс часов</strong><p class="muted">Появится после создания каркаса.</p></div>
<div id="issues" class="summary"><strong>Проверки</strong><p class="muted">Ошибки и предупреждения появятся здесь.</p></div>
<button id="approve" class="primary wide" type="button" disabled>Утвердить каркас</button>
<button id="generate" class="primary wide" type="button" disabled>Подготовить материалы</button>
<button id="export" class="secondary wide" type="button" disabled>Собрать файлы</button>
<div id="files" class="files"></div>
<div class="summary"><strong>Открыть программу</strong><label for="program-id">Идентификатор</label><input id="program-id" placeholder="ID программы"><button id="open" class="secondary wide" type="button">Открыть</button></div>
<div id="usage" class="summary muted">Стоимость пока не рассчитана.</div>
</aside></div></div><script src="/static/app.js" type="module"></script></body></html>`))

func (a App) home(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = homeTemplate.Execute(w, struct{ Embed, Demo bool }{a.Embed, a.Demo})
}
