(() => {
  'use strict';
  let program = null;
  let busy = false;
  const $ = id => document.getElementById(id);
  const states = {outline_ready:'Каркас готов к утверждению',approved:'Каркас утвержден',ready:'Комплект готов к проверке методистом'};
  const kinds = {lecture:'Лекция',practice:'Практика',laboratory:'Лабораторная',self_study:'Самостоятельная работа'};
  const node = (tag, value, className) => {
    const n = document.createElement(tag);
    if (value !== undefined && value !== null) n.textContent = String(value);
    if (className) n.className = className;
    return n;
  };
  const append = (parent, ...children) => {children.forEach(child => parent.appendChild(child));return parent};
  const section = (title) => append(node('section', null, 'block'), node('h3', title));
  const fmt = value => Number(value || 0).toLocaleString('ru-RU', {maximumFractionDigits:2});

  function setBusy(value) {
    busy = value;
    ['create','open','read-reference'].forEach(id => $(id).disabled = busy);
    $('edit-apply').disabled = busy || !program;
    $('approve').disabled = busy || program?.state !== 'outline_ready';
    $('generate').disabled = busy || program?.state !== 'approved';
    $('export').disabled = busy || program?.state !== 'ready' || program.approved_revision !== program.outline_revision;
  }

  async function call(action, extra = {}) {
    const response = await fetch('/v1/agent', {
      method:'POST',
      headers:{'Content-Type':'application/json','Authorization':'Bearer ' + $('token').value},
      body:JSON.stringify({action, ...extra})
    });
    const data = await response.json();
    if (!response.ok) throw new Error(data.error || 'Ошибка запроса');
    return data;
  }

  function renderQuestions(questions) {
    const box = $('questions');
    box.replaceChildren();
    box.hidden = !questions || !questions.length;
    if (!box.hidden) {
      append(box, node('strong','Нужны уточнения'),node('p','Дополните параметры слева и снова подготовьте каркас.'));
      const list = node('ol');
      questions.forEach(q => list.appendChild(node('li',q)));
      box.appendChild(list);
    }
  }

  function renderHours(p) {
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

  function renderIssues(issues) {
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

  function renderSteps(state) {
    const current = {outline_ready:1,approved:2,ready:4}[state] ?? 0;
    [...$('steps').children].forEach((item,index) => {
      item.className = index < current ? 'done' : index === current ? 'current' : '';
    });
  }

  function renderOverview(p) {
    const root = $('overview');
    root.replaceChildren();
    const goal = section('Цель программы');
    goal.appendChild(node('p',p.goal || 'Цель пока не указана.'));
    root.appendChild(goal);

    const outcomes = section('Карта компетенций и результатов');
    (p.outcomes || []).forEach(o => {
      const item = node('article',null,'item');
      append(item,node('h3',o.text),node('div','Темы: ' + (o.topic_ids || []).join(', '),'meta'));
      const tags = node('div',null,'tags');
      [['Знания',o.knowledge],['Умения',o.skills],['Навыки',o.experience]].forEach(([label,values]) => (values || []).forEach(v => tags.appendChild(node('span',label + ': ' + v,'tag'))));
      if (tags.childElementCount) item.appendChild(tags);
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
        append(item,node('h3',s.title),node('p',s.evidence || 'Подтверждающий фрагмент отсутствует'),node('div',(s.location || '') + ' · ' + (s.status === 'verified' ? 'Подтвержден':'Не подтвержден'),'meta'));
        sources.appendChild(item);
      });
      root.appendChild(sources);
    }
  }

  function editText(p, target) {
    const parts = target.split(':');
    if (parts[0] === 'goal') return p.goal;
    if (parts[0] === 'outcome') {
      const value = p.outcomes[Number(parts[1])][parts[2]];
      return Array.isArray(value) ? value.join('\n') : value;
    }
    if (parts[0] === 'topic') return p.modules[Number(parts[1])].topics[Number(parts[2])][parts[3]];
    if (parts[0] === 'activity') return p.modules[Number(parts[1])].topics[Number(parts[2])].activities[Number(parts[3])].hours;
    if (parts[0] === 'assessment') return p.assessments[Number(parts[1])][parts[2]] || '';
    if (parts[0] === 'lecture') return parts[2] === 'slides' ? (p.lectures[Number(parts[1])].slides || []).join('\n') : p.lectures[Number(parts[1])].plan;
    return '';
  }

  function setEditText(p, target, value) {
    const parts = target.split(':');
    if (parts[0] === 'goal') p.goal = value;
    if (parts[0] === 'outcome') p.outcomes[Number(parts[1])][parts[2]] = parts[2] === 'text' ? value : value.split('\n').map(x => x.trim()).filter(Boolean);
    if (parts[0] === 'topic') p.modules[Number(parts[1])].topics[Number(parts[2])][parts[3]] = value;
    if (parts[0] === 'activity') {
      const hours = Number(value.replace(',','.'));
      if (!Number.isFinite(hours) || hours < 0) throw new Error('Часы должны быть неотрицательным числом.');
      p.modules[Number(parts[1])].topics[Number(parts[2])].activities[Number(parts[3])].hours = hours;
    }
    if (parts[0] === 'assessment') p.assessments[Number(parts[1])][parts[2]] = value;
    if (parts[0] === 'lecture') p.lectures[Number(parts[1])][parts[2]] = parts[2] === 'slides' ? value.split('\n').map(x => x.trim()).filter(Boolean) : value;
  }

  function renderEditor(p) {
    $('editor').hidden = false;
    const select = $('edit-target');
    const previous = select.value;
    select.replaceChildren();
    const add = (value,label) => {const option = node('option',label);option.value = value;select.appendChild(option)};
    add('goal','Цель программы');
    (p.outcomes || []).forEach((o,i) => {
      add('outcome:' + i + ':text','Результат: ' + o.id);
      add('outcome:' + i + ':knowledge','Знания: ' + o.id);
      add('outcome:' + i + ':skills','Умения: ' + o.id);
      add('outcome:' + i + ':experience','Навыки: ' + o.id);
    });
    (p.modules || []).forEach((m,mi) => (m.topics || []).forEach((t,ti) => {
      add('topic:' + mi + ':' + ti + ':title','Тема: ' + t.title);
      add('topic:' + mi + ':' + ti + ':summary','Описание темы: ' + t.title);
      (t.activities || []).forEach((a,ai) => add('activity:' + mi + ':' + ti + ':' + ai + ':hours','Часы: ' + t.title + ' · ' + a.name));
    }));
    (p.assessments || []).forEach((a,i) => {
      add('assessment:' + i + ':question','Вопрос/задание: ' + a.id);
      if (a.kind === 'test') add('assessment:' + i + ':answer','Ключ: ' + a.id);
      else add('assessment:' + i + ':rubric','Критерии: ' + a.id);
    });
    (p.lectures || []).forEach((l,i) => {add('lecture:' + i + ':plan','План лекции: ' + l.topic_id);add('lecture:' + i + ':slides','Слайды: ' + l.topic_id)});
    if ([...select.options].some(option => option.value === previous)) select.value = previous;
    $('edit-value').value = editText(p,select.value);
  }

  function show(data) {
    if (data.questions?.length && !data.program) program = null;
    if (data.program) program = data.program;
    renderQuestions(data.questions);
    if (!program) {
      $('canvas-title').textContent = 'Нужны параметры программы';
      $('revision').textContent = 'Новый запрос';
      $('overview').replaceChildren();
      $('files').replaceChildren();
      $('editor').hidden = true;
      $('raw').hidden = true;
      $('result').textContent = '';
      $('program-id').value = '';
      $('hours').replaceChildren(node('strong','Баланс часов'),node('p','Появится после подготовки каркаса.','muted'));
      $('issues').replaceChildren(node('strong','Проверки'),node('p','Сначала дополните параметры программы.','muted'));
      $('usage').textContent = 'Стоимость пока не рассчитана.';
      renderSteps(null);
      history.replaceState(null,'',location.pathname);
      $('state').textContent = data.questions?.length ? 'Дополните условия запроса.' : 'Программа еще не создана.';
      $('state').className = 'muted';
      setBusy(busy);
      return;
    }
    $('canvas-title').textContent = program.spec.title;
    $('revision').textContent = 'Версия ' + program.revision;
    $('state').textContent = states[program.state] || program.state;
    $('state').className = 'muted';
    $('program-id').value = program.id;
    $('approve').textContent = 'Утвердить каркас v' + program.outline_revision;
    $('approve').disabled = busy || program.state !== 'outline_ready';
    $('generate').disabled = busy || program.state !== 'approved';
    $('export').disabled = busy || program.state !== 'ready';
    renderOverview(program);
    renderEditor(program);
    renderHours(program);
    renderIssues(data.issues || program.issues || []);
    renderSteps(program.state);
    $('usage').textContent = 'Токены: ' + ((program.usage?.prompt_tokens || 0) + (program.usage?.completion_tokens || 0)).toLocaleString('ru-RU') + '. Стоимость пока не рассчитана.';
    $('result').textContent = JSON.stringify(program,null,2);
    $('raw').hidden = false;
    $('files').replaceChildren();
    history.replaceState(null,'','?program_id=' + encodeURIComponent(program.id));
  }

  async function run(action,extra = {}) {
    if (busy) return;
    setBusy(true);
    $('state').textContent = 'Выполняется: ' + ({create:'подготовка каркаса',approve_outline:'утверждение',generate:'подготовка материалов',status:'загрузка программы'}[action] || action) + '…';
    $('state').className = 'muted';
    try { show(await call(action,extra)); }
    catch (e) { $('state').textContent = e.message; $('state').className = 'error'; }
    finally {
      setBusy(false);
    }
  }

  $('create').onclick = () => run('create',{spec:{
    title:$('title').value,kind:$('kind').value,requested:Number($('requested').value),
    audience:$('audience').value,entry_level:$('level').value,delivery:$('delivery').value,
    attestation:$('attestation').value,independent_internship:$('internship').checked,
    requirements:$('requirements').value
  }});
  $('read-reference').onclick = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const file = $('reference').files[0];
      if (!file) throw new Error('Выберите файл.');
      const form = new FormData();form.append('file',file);
      const response = await fetch('/v1/references',{method:'POST',headers:{'Authorization':'Bearer ' + $('token').value},body:form});
      const data = await response.json();
      if (!response.ok) throw new Error(data.error || 'Ошибка чтения файла');
      $('requirements').value += '\n\nРеференс ' + data.name + ' (SHA-256 ' + data.sha256 + '):\n' + data.text;
      $('state').textContent = 'Текст файла добавлен к требованиям.';
      $('state').className = 'muted';
    } catch (e) {$('state').textContent = e.message;$('state').className = 'error';}
    finally {setBusy(false);}
  };
  $('approve').onclick = () => program && run('approve_outline',{program_id:program.id,revision:program.revision});
  $('generate').onclick = async () => {
    if (!program || busy) return;
    setBusy(true);
    try {
      let response = await call('generate_async',{program_id:program.id,revision:program.revision});
      let job = response.job;
      const deadline = Date.now() + 35 * 60 * 1000;
      $('state').textContent = 'Задание поставлено в очередь · ' + job.id;
      $('state').className = 'muted';
      while (job.state === 'queued' || job.state === 'running') {
        if (Date.now() > deadline) throw new Error('Задание ещё выполняется. Откройте программу позже, чтобы проверить результат.');
        await new Promise(resolve => setTimeout(resolve,1500));
        response = await call('job_status',{job_id:job.id});
        job = response.job;
        $('state').textContent = job.state === 'running' ? 'Подготовка материалов выполняется…' : 'Ожидание свободного исполнителя…';
      }
      if (job.state === 'failed') {show(response);throw new Error(job.error || 'Генерация не завершилась')}
      show(response);
    } catch (e) {$('state').textContent = e.message;$('state').className = 'error';}
    finally {
      setBusy(false);
    }
  };
  $('open').onclick = () => run('status',{program_id:$('program-id').value.trim()});
  $('edit-target').onchange = () => {if (program) $('edit-value').value = editText(program,$('edit-target').value)};
  $('edit-apply').onclick = () => {
    if (!program) return;
    const proposed = structuredClone(program);
    try {setEditText(proposed,$('edit-target').value,$('edit-value').value.trim())}
    catch (e) {$('state').textContent = e.message;$('state').className = 'error';return}
    run('revise',{program_id:program.id,revision:program.revision,program:proposed});
  };
  $('export').onclick = async () => {
    if (!program || busy) return;
    setBusy(true);
    $('state').textContent = 'Сборка файлов…';
    try {
      const data = await call('export',{program_id:program.id,revision:program.revision});
      const files = $('files');files.replaceChildren();
      for (const [name,url] of Object.entries(data.artifacts || {})) {
        const link = node('a','Скачать ' + name);link.href = '#';
        link.onclick = async event => {
          event.preventDefault();
          try {
            const response = await fetch(url,{headers:{'Authorization':'Bearer ' + $('token').value}});
            if (!response.ok) throw new Error('Не удалось скачать ' + name);
            const blob = await response.blob();const anchor = document.createElement('a');
            anchor.href = URL.createObjectURL(blob);anchor.download = name;anchor.click();
            setTimeout(() => URL.revokeObjectURL(anchor.href),1000);
          } catch (e) {$('state').textContent = e.message;$('state').className = 'error';}
        };
        files.appendChild(link);
      }
      $('state').textContent = 'Файлы версии ' + program.revision + ' собраны.';
    } catch (e) {$('state').textContent = e.message;$('state').className = 'error';}
    finally {setBusy(false);}
  };
  const initialID = new URLSearchParams(location.search).get('program_id');
  if (initialID) $('program-id').value = initialID;
})();
