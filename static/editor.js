import {node} from './views.js';
const $ = id => document.getElementById(id);
export function editText(p, target) {
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
export function setEditText(p, target, value) {
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

export function renderEditor(p) {
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
