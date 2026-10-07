import {
  YueStatus, YueStats, YueJobs, YueSubmit, YueSubmitFan, YuePlan, YueCancelJob, YueAudioURL,
  YueOpenExternal, YueSaveAudio, YueOpenURL, YueGetServerURL, YueSetServerURL,
  YueWorkerConfig, YueSetWorkerConfig, YueOllamaModels,
  YueAnalyzeJob, YueReferences, YueAddReference, YueDspChains, YueApplyDsp, YueFxPreview, YuePlayPreview, YueApplySteps, YueDspPresets, YueDspVariants,
  YueCopilot,
  YueTranscribeFile, YueTranscribeJob, YueImportTrack, YueEnsureMp3, YueJobScore, YueJobAbcText, YueJobPreview, YueSubmitOverdub,
  YueJobPeaks, YueJobSpectrumPNG,
  YueRecognizeLyricsFile, YueAdaptLyrics, YueJobLyrics,
  YueMakeStems, YueJobStems, YueMakeMinus, YueApplyFx, YueFxAssets, YueUploadFxAsset, YueInstallFxKit,
  YueCorpusCreate, YueCorpusAddTracks, YueCorpusBuild, YueCorpusList, YueCorpusGet, YueCorpusTracks,
  YueVoiceCreate, YueVoices, YueVoiceDelete, YueVariantToTrack, YueVocalContour, YueJobTones, YuePlanCheck, YueJobGrid, YueSplice, YueContinueJob, YueSetHead, YueRenameJob, YueRetryJob, YueSetJobFolder, YueVoiceConvert, YueRebuildSections, YueDspVariantDelete, YueVolumeEnvelope,
  YuePlayFile, YueToggleAudio, YueStopAudio, YueSeekAudio, YueSetVolume, YuePlayAudio, YueAudioState, YueTranslate, YueDeleteJob,
} from './wailsjs/go/main/App'

export const api = {
  status: () => YueStatus(),
  stats: () => YueStats(),
  jobs: () => YueJobs(),
  submit: (p) => YueSubmit(p),
  submitFan: (p, n) => YueSubmitFan(p, n),
  plan: (p) => YuePlan(p),
  cancel: (id) => YueCancelJob(id),
  deleteJob: (id) => YueDeleteJob(id),
  audioURL: (id, file) => YueAudioURL(id, file),
  openExternal: (id, file) => YueOpenExternal(id, file),
  openURL: (url) => YueOpenURL(url),
  saveAudio: (id, file) => YueSaveAudio(id, file),
  getServerURL: () => YueGetServerURL(),
  setServerURL: (url) => YueSetServerURL(url),
  workerConfig: () => YueWorkerConfig(),
  setWorkerConfig: (cfg) => YueSetWorkerConfig(cfg),
  ollamaModels: (url) => YueOllamaModels(url || ''),
  analyze: (id) => YueAnalyzeJob(id),
  references: () => YueReferences(),
  addReference: () => YueAddReference(),
  dspChains: () => YueDspChains(),
  applyDsp: (id, chain, params) => YueApplyDsp(id, chain, params),
  dspVariants: (id) => YueDspVariants(id),
  // звуковой движок воркера: req = {source, chain: [{type, …}], from?, to?, output?, label?}
  applyFx: (id, req) => YueApplyFx(id, req),
  fxAssets: () => YueFxAssets(),
  uploadFxAsset: (kind) => YueUploadFxAsset(kind),
  installFxKit: (name) => YueInstallFxKit(name),
  // быстрое превью эффектов (steps — цепочка по порядку) на куске трека; куски
  // «было/стало» остаются на ПК, играет playPreview (slot '' или A–D, which wet/dry)
  fxPreview: (id, stem, steps, from, to, slot = '') => YueFxPreview(id, stem, steps, from, to, slot),
  playPreview: (id, slot, which, startSec = 0) => YuePlayPreview(id, slot, which, startSec),
  // доска педалей на весь трек (вариант dsp-pedals.flac) и готовые наборы
  applySteps: (id, steps, label = '') => YueApplySteps(id, steps, label),
  dspPresets: () => YueDspPresets(),
  copilot: (p) => YueCopilot(p),
  // v2/v3
  transcribeFile: () => YueTranscribeFile(),
  importTrack: () => YueImportTrack(),
  transcribeJob: (id) => YueTranscribeJob(id),
  ensureMp3: (id) => YueEnsureMp3(id),
  jobScore: (id) => YueJobScore(id),
  // волна громкости: file '' — основной трек, bins 0 — каноническое разрешение
  jobPeaks: (id, file, bins) => YueJobPeaks(id, file || '', bins || 0),
  // спектрограмма PNG (base64 от воркера) — data-URL для <img>
  jobSpectrumPNG: (id, file) => YueJobSpectrumPNG(id, file || ''),
  jobAbcText: (id, file) => YueJobAbcText(id, file),
  jobPreview: (id, from, to) => YueJobPreview(id, from, to),
  submitOverdub: (id, style, lyrics, gain, abc, seed) => YueSubmitOverdub(id, style, lyrics, gain, abc || '', seed || 0),
  recognizeLyrics: () => YueRecognizeLyricsFile(),
  jobLyrics: (id) => YueJobLyrics(id),
  adaptLyrics: (text, to) => YueAdaptLyrics(text, to || 'Russian'),
  makeStems: (id) => YueMakeStems(id),
  jobStems: (id) => YueJobStems(id),
  makeMinus: (id, exclude) => YueMakeMinus(id, exclude || []),
  corpusCreate: (name) => YueCorpusCreate(name),
  corpusAddTracks: (id) => YueCorpusAddTracks(id),
  corpusBuild: (id) => YueCorpusBuild(id),
  corpusList: () => YueCorpusList(),
  corpusGet: (id) => YueCorpusGet(id),
  corpusTracks: (id) => YueCorpusTracks(id),
  voiceCreate: (name, jobId, params, seed) => YueVoiceCreate(name, jobId, params, seed),
  voices: () => YueVoices(),
  voiceDelete: (id) => YueVoiceDelete(id),
  // все замены дорожек трека заново с чистого оригинала:
  // [{child_id, from, to, lead, beat_sec, db, stems, fade_in, fade_out}]
  rebuildSections: (parentId, specs) => YueRebuildSections(parentId, specs),
  // линия громкости по волне: stem '' — весь трек (вариант), иначе только дорожка
  volumeEnvelope: (id, stem, points) => YueVolumeEnvelope(id, stem || '', points),
  dspVariantDelete: (id, file) => YueDspVariantDelete(id, file),
  variantToTrack: (jobId, file, title, voiceSrc = 0) => YueVariantToTrack(jobId, file, title, voiceSrc),
  // высота голоса по тактам плана: {bars:[{index,start,end,notes}], median_hz, low_hz, high_hz}
  vocalContour: (jobId, from = 0, to = 0) => YueVocalContour(jobId, from, to),
  // узкие тона («свист») в окне: [{hz, prominence_db}], самый заметный первым
  jobTones: (jobId, from = 0, to = 0, stem = '') => YueJobTones(jobId, from, to, stem),
  jobGrid: (jobId, from = 0, to = 0) => YueJobGrid(jobId, from, to),
  // что изменилось в плане и где проблемы (потолок голоса, правки до отметки)
  planCheck: (jobId, abc, fromSec = 0) => YuePlanCheck(jobId, abc, fromSec),
  // склейка кусков версий [{job_id, from, to, gain_db}] → вариант трека baseId
  splice: (baseId, parts, crossfade = 0) => YueSplice(baseId, parts, crossfade),
  // трек до fromSec + продолжение моделью (seed 0 — случайный; abc — план; styleAdd — звучание)
  continueJob: (jobId, fromSec, seed, abc, styleAdd) => YueContinueJob(jobId, fromSec, seed || 0, abc || '', styleAdd || ''),
  // основная версия песни: rootId — корень, headId — версия (0 — сам трек)
  setHead: (rootId, headId) => YueSetHead(rootId, headId || 0),
  renameJob: (id, title) => YueRenameJob(id, title),
  retryJob: (id) => YueRetryJob(id),
  setJobFolder: (id, folder) => YueSetJobFolder(id, folder || ''),
  // «голос альбома»: { ref_job_id, ref_from?, ref_dur?, steps?, title? } → id джобы
  voiceConvert: (id, params) => YueVoiceConvert(id, params),
  playFile: (id, file, dur) => YuePlayFile(id, file, dur),
  playAudio: (id) => YuePlayAudio(id),
  translate: (text) => YueTranslate(text),
  stopAudio: () => YueStopAudio(),
  toggleAudio: () => YueToggleAudio(),
  seekAudio: (sec) => YueSeekAudio(sec),
  audioState: () => YueAudioState(),
  setVolume: (v) => YueSetVolume(v),
}
