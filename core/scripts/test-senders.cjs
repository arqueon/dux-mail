const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../../quickshell/Common/Senders.js'), 'utf8');
const api = vm.runInNewContext(
    source.replace(/^\.pragma library\s*/, '') +
    '\n({decodeMimeWords, parseAddress, displayName, isSelf, shortName, formatThreadSenders})'
);

const meAcct = 'ruben.garcia.sanchez@udg.mx';
const meFull = 'RUBEN JUAN SEBASTIAN GARCIA SANCHEZ <ruben.garcia.sanchez@udg.mx>';

test('single-message thread preserves full display name without count', () => {
    const res = api.formatThreadSenders({
        participants: ['infonavitmascercadeti <comunicacion@infonavit.org.mx>'],
        lastSender: 'infonavitmascercadeti <comunicacion@infonavit.org.mx>',
        messageCount: 1
    }, meAcct, 'yo', false);
    assert.equal(res.prefix, '');
    assert.equal(res.latest, 'infonavitmascercadeti');
    assert.equal(res.fullText, 'infonavitmascercadeti');
    assert.equal(res.count, 0);
    assert.equal(res.avatarRaw, 'infonavitmascercadeti <comunicacion@infonavit.org.mx>');
    assert.equal(res.tooltip, '');
});

test('self -> other (#23082 shape) shows yo prefix, full latest sender, count 2, and other avatar', () => {
    const martin = 'MARTIN PIZANO MORENO <martin.pizano@udg.mx>';
    const res = api.formatThreadSenders({
        participants: [meFull, martin],
        lastSender: martin,
        messageCount: 2
    }, meAcct, 'yo', false);
    assert.equal(res.prefix, 'yo, ');
    assert.equal(res.latest, 'MARTIN PIZANO MORENO');
    assert.equal(res.fullText, 'yo, MARTIN PIZANO MORENO');
    assert.equal(res.count, 2);
    assert.equal(res.avatarRaw, martin);
    assert.equal(res.tooltip, 'yo, MARTIN PIZANO MORENO (2)');
});

test('long institutional name with commas (#22930 shape) shortens to first word', () => {
    const jornadas = '"Jornadas Spinozianas, Facultad Filosofía, UMSNH" <jornadas.spinozianas@umich.mx>';
    const abdel = 'ABDEL VIDRIO REYNOSO <abdel@cgpe.udg.mx>';
    const res = api.formatThreadSenders({
        participants: [jornadas, abdel],
        lastSender: abdel,
        messageCount: 2
    }, meAcct, 'yo', false);
    assert.equal(res.prefix, 'Jornadas, ');
    assert.equal(res.latest, 'ABDEL VIDRIO REYNOSO');
    assert.equal(res.count, 2);
    assert.equal(res.avatarRaw, abdel);
});

test('round-trip A -> self -> A (#23105 shape) puts A at end via lastSender', () => {
    const melanie = 'MELANIE REBECA ESTRADA SANCHEZ <melanie.estrada2155@alumnos.udg.mx>';
    const selfBare = '<ruben.gsanchez@academicos.udg.mx>';
    const res = api.formatThreadSenders({
        participants: [melanie, selfBare],
        lastSender: melanie,
        messageCount: 3
    }, 'ruben.gsanchez@academicos.udg.mx', 'yo', false);
    assert.equal(res.prefix, 'yo, ');
    assert.equal(res.latest, 'MELANIE REBECA ESTRADA SANCHEZ');
    assert.equal(res.count, 3);
    assert.equal(res.avatarRaw, melanie);
    assert.equal(res.tooltip, 'MELANIE REBECA ESTRADA SANCHEZ, yo (3)');
});

test('other -> self (#23199 shape) picks latest non-self participant for avatar', () => {
    const miguel = 'Miguel Angel Sigala Gomez <msigalag@asu.edu>';
    const res = api.formatThreadSenders({
        participants: [miguel, meFull],
        lastSender: meFull,
        messageCount: 2
    }, meAcct, 'yo', false);
    assert.equal(res.prefix, 'Miguel, ');
    assert.equal(res.latest, 'yo');
    assert.equal(res.fullText, 'Miguel, yo');
    assert.equal(res.count, 2);
    assert.equal(res.avatarRaw, miguel);
});

test('many participants (#22976 shape) collapses middle with ellipsis', () => {
    const macarena = 'Macarena Francisca Williamson Modesto <macawilliamson@gmail.com>';
    const res = api.formatThreadSenders({
        participants: [
            'Ernesto Antonio Zarco Ortiz <ezarco@colmex.mx>',
            'Lucas Ruppel <lruppel@ucdavis.edu>',
            '"René A. Tec-López" <rene.teclopez@gmail.com>',
            'KENIA BERENICE ORTIZ CADENA <keniaortiz@cucea.udg.mx>',
            macarena
        ],
        lastSender: macarena,
        messageCount: 11
    }, meAcct, 'yo', false);
    assert.equal(res.prefix, 'Ernesto, …, ');
    assert.equal(res.latest, 'Macarena Francisca Williamson Modesto');
    assert.equal(res.count, 11);
    assert.equal(res.avatarRaw, macarena);
});

test('same sender repeated (#23103 shape) keeps single name and shows message count', () => {
    const res = api.formatThreadSenders({
        participants: ['Notificaciones <notificaciones@banamex.com>'],
        lastSender: 'Notificaciones <notificaciones@banamex.com>',
        messageCount: 3
    }, meAcct, 'yo', false);
    assert.equal(res.prefix, '');
    assert.equal(res.latest, 'Notificaciones');
    assert.equal(res.count, 3);
    assert.equal(res.tooltip, 'Notificaciones (3)');
});

test('self-only thread (#23186 shape) shows localized me and count', () => {
    const res = api.formatThreadSenders({
        participants: [meFull],
        lastSender: meFull,
        messageCount: 3
    }, meAcct, 'yo', false);
    assert.equal(res.prefix, '');
    assert.equal(res.latest, 'yo');
    assert.equal(res.count, 3);
    assert.equal(res.avatarRaw, meFull);
});

test('shared relay email with distinct display names remains two participants', () => {
    const bot = '"github-actions[bot]" <notifications@github.com>';
    const ilias = 'Ilias Beshimov <notifications@github.com>';
    const res = api.formatThreadSenders({
        participants: [bot, ilias],
        lastSender: ilias,
        messageCount: 2
    }, meAcct, 'yo', false);
    assert.equal(res.prefix, 'github-action…, ');
    assert.equal(res.latest, 'Ilias Beshimov');
    assert.equal(res.count, 2);
});

test('sent view displays recipients without converting to yo', () => {
    const res = api.formatThreadSenders({
        participants: ['Ada Lovelace <ada@example.org>', meFull],
        lastSender: '',
        messageCount: 2
    }, meAcct, 'yo', true);
    assert.equal(res.prefix, 'Ada, ');
    assert.equal(res.latest, 'RUBEN JUAN SEBASTIAN GARCIA SANCHEZ');
    assert.equal(res.count, 2);
    assert.equal(res.avatarRaw, 'Ada Lovelace <ada@example.org>');
});

test('shortName handles titles, bare emails, and RFC 2047 encoded words', () => {
    assert.equal(api.shortName('Dr. Ada Lovelace <ada@x.org>', '', 'me'), 'Dr. Ada');
    assert.equal(api.shortName('alice@example.org', '', 'me'), 'alice');
    assert.equal(
        api.decodeMimeWords('=?UTF-8?B?TWFydMOtbiBQaXphbm8=?= <m@udg.mx>'),
        'Martín Pizano <m@udg.mx>'
    );
    assert.equal(
        api.decodeMimeWords('=?UTF-8?Q?Ren=C3=A9_A=2E_100%?= <r@x.org>'),
        'René A. 100% <r@x.org>'
    );
});
