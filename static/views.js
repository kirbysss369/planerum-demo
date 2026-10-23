const $ = id => document.getElementById(id);
export const node = (tag, value, className) => {
  const n = document.createElement(tag);
  if (value !== undefined && value !== null) n.textContent = String(value);
  if (className) n.className = className;
  return n;
};
const append = (parent, ...children) => {children.forEach(child => parent.appendChild(child)); return parent;};
const section = title => append(node('section', null, 'block'), node('h3', title));
const fmt = value => Number(value || 0).toLocaleString('ru-RU', {maximumFractionDigits:2});
const kinds = {lecture:'Лекция',practice:'Практика',laboratory:'Лабораторная',self_study:'Самостоятельная работа'};
export function renderHours(p) {
    const box = $('hours');
    box.replaceChildren(node('strong','Баланс часов'));
    const totals = {lecture:0,practice:0,laboratory:0,self_study:0};
    (p.modules || []).forEach(m => (m.topics || []).forEach(t => (t.activities || []).forEach(a => {
      if (Object.hasOwn(totals,a.kind)) totals[a.kind] += Number(a.hours || 0);
    })));
    const total = Object.values(totals).reduce((a,b) => a+b,0) + Number(p.final_attestation_hours || 0);
    const target = Number(p.spec.requested || 0);
    box.appendChild(node('div',fmt(total) + ' из ' + fmt(target) + ' ч','metric'));
    box.appendChild(node('p',Math.abs(total-target)<0.000001?'Часы сходятся.':'Расхождение: ' + fmt(total-target) + ' ч.',Math.abs(total-target)<0.000001?'muted':'error'));
    const grid = node('div',null,'breakdown');
    [['lecture','Лекции'],['practice','Практика'],['laboratory','Лабораторные'],['self_study','Самостоятельная работа']].forEach(([key,label]) => {
      append(grid,node('span',label),node('span',fmt(totals[key]) + ' ч'));
    });
    append(grid,node('span','Итоговая аттестация'),node('span',fmt(p.final_attestation_hours) + ' ч'));
    box.appendChild(grid);
}
export function renderIssues(issues) {
    const box = $('issues');
    box.replaceChildren(node('strong','Проверки'));
    if (!issues || !issues.length) {
      box.appendChild(node('p','Структурные проверки пройдены. Содержание должен проверить методист.','muted'));
      return;
    }
    const errors = issues.filter(i => i.severity === 'error').length;
    box.appendChild(node('p',errors + ' ошибок · ' + (issues.length-errors) + ' предупреждений','muted'));
    issues.forEach(i => {
      const item = node('div',null,i.severity === 'warning' ? 'issue warning':'issue');
      append(item,node('span',i.message),node('small',i.path));
      box.appendChild(item);
    });
  }

export function renderSteps(state) {
    const current = {outline_ready:1,approved:2,ready:4}[state] ?? 0;
    [...$('steps').children].forEach((item,index) => {
      item.className = index < current ? 'done' : index === current ? 'current' : '';
    });
  }

export function renderOverview(p, coverage = []) {
    const root = $('overview');
    root.replaceChildren();
    const goal = section('Цель программы');
    goal.appendChild(node('p',p.goal || 'Цель пока не указана.'));
    root.appendChild(goal);

    const outcomes = section('Карта компетенций и результатов');
    const labels = items => (items || []).map(item => item.id + ' — ' + item.text).join('; ') || 'Не подготовлены';
    if (coverage.length) {
      const wrap = node('div', null, 'table-wrap');
      const table = node('table');
      const head = node('tr');
      ['Компетенция', 'Темы', 'Тестовые вопросы', 'Практические задания', 'Покрытие'].forEach(label => head.appendChild(node('th', label)));
      table.appendChild(append(node('thead'), head));
      const body = node('tbody');
      for (const row of coverage) {
        const tr = node('tr');
        [row.outcome_id + ' — ' + row.outcome_text, labels(row.topics), labels(row.tests), labels(row.practice), row.complete ? 'Связи заполнены' : p.state === 'ready' ? 'Есть пробелы' : 'Материалы ещё не завершены'].forEach(value => tr.appendChild(node('td', value)));
        body.appendChild(tr);
      }
      table.appendChild(body);wrap.appendChild(table);outcomes.appendChild(wrap);
    }
    outcomes.appendChild(node('p', 'Проверяются наличие и взаимность ссылок. Корректность содержания и соответствие заданий компетенциям проверяет методист.', 'muted'));
    (p.outcomes || []).forEach(o => {
      const item = node('article',null,'item');
      append(item,node('h3',o.id + ' — ' + o.text));
      const tags = node('div',null,'tags');
      [['Знания',o.knowledge],['Умения',o.skills],['Навыки',o.experience]].forEach(([label,values]) => (values || []).forEach(v => tags.appendChild(node('span',label + ': ' + v,'tag'))));
      if (tags.childElementCount) item.appendChild(tags);
      if (o.standard_code) item.appendChild(node('p', 'Трудовая функция: ' + o.standard_code + ' · источник ' + o.source_id));
      outcomes.appendChild(item);
    });
    root.appendChild(outcomes);

    const plan = section('Учебно-тематический план');
    (p.modules || []).forEach(m => {
      const module = node('article',null,'item');
      module.appendChild(node('h3',m.title));
      (m.topics || []).forEach(t => {
        append(module,node('p',t.title),node('div',t.summary || '','meta'));
        const wrap = node('div',null,'table-wrap');
        const table = node('table');
        const head = node('thead');
        const heading = node('tr');
        ['Вид работы','Занятие','Часы'].forEach(label => heading.appendChild(node('th',label)));
        head.appendChild(heading);
        const body = node('tbody');
        (t.activities || []).forEach(a => {
          const row = node('tr');
          append(row,node('td',kinds[a.kind] || a.kind),node('td',a.name),node('td',fmt(a.hours)));
          body.appendChild(row);
        });
        append(table,head,body);wrap.appendChild(table);module.appendChild(wrap);
      });
      plan.appendChild(module);
    });
    root.appendChild(plan);

    if (p.assessments && p.assessments.length) {
      const assessment = section('Оценочные средства');
      p.assessments.forEach(a => {
        const item = node('article',null,'item');
        append(item,node('h3',a.kind === 'test' ? 'Тестовый вопрос' : 'Практическое задание'),node('p',a.question),node('div','Результаты: ' + (a.outcome_ids || []).join(', '),'meta'));
        if (a.answer) item.appendChild(node('p','Ответ: ' + a.answer));
        if (a.rubric) item.appendChild(node('p','Критерии оценки: ' + a.rubric));
        assessment.appendChild(item);
      });
      root.appendChild(assessment);
    }
    if (p.lectures && p.lectures.length) {
      const lectures = section('Лекции и слайды');
      p.lectures.forEach(l => {
        const item = node('article',null,'item');
        const count = (l.slides || []).length;
        append(item,node('h3','Тема ' + l.topic_id),node('p',l.plan),node('div',String(count) + (count === 1 ? ' слайд' : count < 5 ? ' слайда' : ' слайдов'),'meta'));
        lectures.appendChild(item);
      });
      root.appendChild(lectures);
    }
    if (p.sources && p.sources.length) {
      const sources = section('Источники');
      p.sources.forEach(s => {
        const item = node('article',null,'item');
        append(item,node('h3',s.title),node('p',s.evidence || 'Подтверждающий фрагмент отсутствует'),node('div',(s.location || '') + ' · ' + (s.status === 'verified' ? 'Подтверждён методистом':'Не подтверждён'),'meta'));
        sources.appendChild(item);
      });
      root.appendChild(sources);
    }
  }

export function renderMetrics(p) {
  const box = $('usage');
  const usage = p.usage || {};
  box.replaceChildren(node('strong', 'Измерения генерации'));
  const calls = (p.generation_metrics || []).flatMap(metric => metric.calls || []);
  const models = [...new Set(calls.map(call => (call.mode === 'real' ? 'Реальная модель: ' : call.mode === 'demo' ? 'Демонстрационные данные: ' : 'Режим не подтверждён: ') + (call.model_id || 'не указан')))];
  box.appendChild(node('p', models.length ? models.join('; ') : 'Историческая запись: модель и режим не зафиксированы.'));
  if (p.origin) box.appendChild(node('p', 'Адаптация программы ' + p.origin.program_id + ', версия ' + p.origin.revision + '. Исходная программа сохранена.'));
  box.appendChild(node('p', 'Токены: вход ' + fmt(usage.prompt_tokens) + ', выход ' + fmt(usage.completion_tokens) + '.'));
  const metrics = p.generation_metrics || [];
  if (metrics.length) {
    const stages = {intake:'Разбор запроса', outline:'Каркас', materials:'Материалы'};
    const wrap = node('div', null, 'table-wrap');
    const table = node('table');
    const head = node('tr');
    ['Этап', 'Секунды', 'Токены: вход / выход'].forEach(text => head.appendChild(node('th', text)));
    table.appendChild(append(node('thead'),head));
    const body = node('tbody');
    metrics.forEach(metric => {
      const row = node('tr');
      [ (stages[metric.stage] || metric.stage) + (metric.succeeded ? '' : ' · ошибка'), fmt(metric.duration_ms / 1000), fmt(metric.usage?.prompt_tokens) + ' / ' + fmt(metric.usage?.completion_tokens) ].forEach(value => row.appendChild(node('td', value)));
      body.appendChild(row);
    });
    table.appendChild(body);wrap.appendChild(table);box.appendChild(wrap);
    box.appendChild(node('p','Время учтённых этапов: ' + fmt(metrics.reduce((sum, metric) => sum + metric.duration_ms, 0) / 1000) + ' с.'));
  } else box.appendChild(node('p','В этой версии программы длительности этапов ещё не записаны.'));
  box.appendChild(node('p','Учёт токенов основан на ответах провайдера. Стоимость определяется его тарифом.','muted'));
}
