/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useState, useRef } from 'react';
import {
  Banner,
  Button,
  Col,
  Descriptions,
  Form,
  Row,
  Spin,
  DatePicker,
  Typography,
  Modal,
} from '@douyinfe/semi-ui';
import dayjs from 'dayjs';
import { useTranslation } from 'react-i18next';
import {
  compareObjects,
  API,
  showError,
  showSuccess,
  showWarning,
} from '../../../helpers';

const { Text } = Typography;
const COSLOG_STATUS_REFRESH_MS = 5000;
const COSLOG_SAMPLE_PRESETS = [0, 1, 10, 25, 50, 100];

function formatBytes(bytes, decimals = 2) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const unitIndex = Math.min(
    Math.floor(Math.log(bytes) / Math.log(1024)),
    units.length - 1,
  );
  const value = bytes / Math.pow(1024, unitIndex);
  return `${value.toFixed(unitIndex === 0 ? 0 : decimals)} ${units[unitIndex]}`;
}

export default function SettingsLog(props) {
  const { t } = useTranslation();
  const [loading, setLoading] = useState(false);
  const [loadingCleanHistoryLog, setLoadingCleanHistoryLog] = useState(false);
  const [inputs, setInputs] = useState({
    LogConsumeEnabled: false,
    CosLogSamplePercent: 100,
    historyTimestamp: dayjs().subtract(1, 'month').toDate(),
  });
  const [cosLogStatus, setCosLogStatus] = useState(null);
  const [cosLogStatusUnavailable, setCosLogStatusUnavailable] = useState(false);
  const [resettingDropped, setResettingDropped] = useState(false);
  const refForm = useRef();
  const [inputsRow, setInputsRow] = useState(inputs);

  function onSubmit() {
    const updateArray = compareObjects(inputs, inputsRow).filter(
      (item) => item.key !== 'historyTimestamp',
    );

    if (!updateArray.length) return showWarning(t('你似乎并没有修改什么'));
    const requestQueue = updateArray.map((item) => {
      let value = '';
      if (typeof inputs[item.key] === 'boolean') {
        value = String(inputs[item.key]);
      } else {
        value = inputs[item.key];
      }
      return API.put('/api/option/', {
        key: item.key,
        value,
      });
    });
    setLoading(true);
    Promise.all(requestQueue)
      .then((res) => {
        if (requestQueue.length === 1) {
          if (res.includes(undefined)) return;
        } else if (requestQueue.length > 1) {
          if (res.includes(undefined))
            return showError(t('部分保存失败，请重试'));
        }
        showSuccess(t('保存成功'));
        props.refresh();
        fetchCosLogStatus();
      })
      .catch(() => {
        showError(t('保存失败，请重试'));
      })
      .finally(() => {
        setLoading(false);
      });
  }
  async function onCleanHistoryLog() {
    if (!inputs.historyTimestamp) {
      showError(t('请选择日志记录时间'));
      return;
    }

    const now = dayjs();
    const targetDate = dayjs(inputs.historyTimestamp);
    const targetTime = targetDate.format('YYYY-MM-DD HH:mm:ss');
    const currentTime = now.format('YYYY-MM-DD HH:mm:ss');
    const daysDiff = now.diff(targetDate, 'day');

    Modal.confirm({
      title: t('确认清除历史日志'),
      content: (
        <div style={{ lineHeight: '1.8' }}>
          <p>
            <Text>{t('当前时间')}：</Text>
            <Text strong style={{ color: '#52c41a' }}>
              {currentTime}
            </Text>
          </p>
          <p>
            <Text>{t('选择时间')}：</Text>
            <Text strong type='danger'>
              {targetTime}
            </Text>
            {daysDiff > 0 && (
              <Text type='tertiary'>
                {' '}
                ({t('约')} {daysDiff} {t('天前')})
              </Text>
            )}
          </p>
          <div
            style={{
              background: '#fff7e6',
              border: '1px solid #ffd591',
              padding: '12px',
              borderRadius: '4px',
              marginTop: '12px',
              color: '#333',
            }}
          >
            <Text strong style={{ color: '#d46b08' }}>
              ⚠️ {t('注意')}：
            </Text>
            <Text style={{ color: '#333' }}>{t('将删除')} </Text>
            <Text strong style={{ color: '#cf1322' }}>
              {targetTime}
            </Text>
            {daysDiff > 0 && (
              <Text style={{ color: '#8c8c8c' }}>
                {' '}
                ({t('约')} {daysDiff} {t('天前')})
              </Text>
            )}
            <Text style={{ color: '#333' }}> {t('之前的所有日志')}</Text>
          </div>
          <p style={{ marginTop: '12px' }}>
            <Text type='danger'>
              {t('此操作不可恢复，请仔细确认时间后再操作！')}
            </Text>
          </p>
        </div>
      ),
      okText: t('确认删除'),
      cancelText: t('取消'),
      okType: 'danger',
      onOk: async () => {
        try {
          setLoadingCleanHistoryLog(true);
          const res = await API.delete(
            `/api/log/?target_timestamp=${Date.parse(inputs.historyTimestamp) / 1000}`,
          );
          const { success, message, data } = res.data;
          if (success) {
            showSuccess(`${data} ${t('条日志已清理！')}`);
            return;
          } else {
            throw new Error(t('日志清理失败：') + message);
          }
        } catch (error) {
          showError(error.message);
        } finally {
          setLoadingCleanHistoryLog(false);
        }
      },
    });
  }

  async function fetchCosLogStatus() {
    try {
      const res = await API.get('/api/coslog/status');
      if (!res.data.success) throw new Error(res.data.message);
      setCosLogStatus(res.data.data);
      setCosLogStatusUnavailable(false);
    } catch {
      setCosLogStatusUnavailable(true);
    }
  }

  async function resetCosLogDropped() {
    setResettingDropped(true);
    try {
      const res = await API.post('/api/coslog/reset-dropped');
      if (!res.data.success) throw new Error(res.data.message);
      showSuccess(t('丢弃数量已清空'));
      await fetchCosLogStatus();
    } catch (error) {
      showError(error.message || t('清空丢弃数量失败'));
    } finally {
      setResettingDropped(false);
    }
  }

  useEffect(() => {
    const currentInputs = {};
    for (let key in props.options) {
      if (Object.keys(inputs).includes(key)) {
        if (key === 'CosLogSamplePercent') {
          currentInputs[key] = Number(props.options[key]);
        } else {
          currentInputs[key] = props.options[key];
        }
      }
    }
    currentInputs['historyTimestamp'] = inputs.historyTimestamp;
    setInputs(Object.assign(inputs, currentInputs));
    setInputsRow(structuredClone(currentInputs));
    refForm.current.setValues(currentInputs);
  }, [props.options]);

  useEffect(() => {
    fetchCosLogStatus();
    const timer = window.setInterval(
      fetchCosLogStatus,
      COSLOG_STATUS_REFRESH_MS,
    );
    return () => window.clearInterval(timer);
  }, []);

  const cosLogInactive = !cosLogStatus?.enabled || !cosLogStatus?.initialized;
  const lastUploadText = cosLogStatus?.last_successful_upload
    ? dayjs
        .unix(cosLogStatus.last_successful_upload)
        .format('YYYY-MM-DD HH:mm:ss')
    : t('从未');

  return (
    <>
      <Spin spinning={loading}>
        <Form
          values={inputs}
          getFormApi={(formAPI) => (refForm.current = formAPI)}
          style={{ marginBottom: 15 }}
        >
          <Form.Section text={t('日志设置')}>
            <Row gutter={16}>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Form.Switch
                  field={'LogConsumeEnabled'}
                  label={t('启用额度消费日志记录')}
                  size='default'
                  checkedText='｜'
                  uncheckedText='〇'
                  onChange={(value) => {
                    setInputs({
                      ...inputs,
                      LogConsumeEnabled: value,
                    });
                  }}
                />
              </Col>
              <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                <Spin spinning={loadingCleanHistoryLog}>
                  <Form.DatePicker
                    label={t('清除历史日志')}
                    field={'historyTimestamp'}
                    type='dateTime'
                    inputReadOnly={true}
                    onChange={(value) => {
                      setInputs({
                        ...inputs,
                        historyTimestamp: value,
                      });
                    }}
                  />
                  <Text
                    type='tertiary'
                    size='small'
                    style={{ display: 'block', marginTop: 4, marginBottom: 8 }}
                  >
                    {t('将清除选定时间之前的所有日志')}
                  </Text>
                  <Button
                    size='default'
                    type='danger'
                    onClick={onCleanHistoryLog}
                  >
                    {t('清除历史日志')}
                  </Button>
                </Spin>
              </Col>
            </Row>

            <div style={{ marginTop: 20 }}>
              <Banner
                type='info'
                description={t(
                  '按稳定比例保存完整请求和响应内容，修改后无需重启即可生效。0% 不保存，100% 保存所有符合现有 COSLOG 条件的请求。',
                )}
                style={{ marginBottom: 16 }}
              />
              {cosLogStatusUnavailable && (
                <Banner
                  type='danger'
                  description={t('无法读取 COSLOG 运行状态')}
                  style={{ marginBottom: 16 }}
                />
              )}
              {cosLogStatus && cosLogInactive && (
                <Banner
                  type='warning'
                  description={t(
                    'COSLOG 未启用或启动时未初始化。现在可以先保存比例，但只有设置 COSLOG_ENABLED=true 并重启后才会开始采集。',
                  )}
                  style={{ marginBottom: 16 }}
                />
              )}
              <Row gutter={16}>
                <Col xs={24} sm={12} md={8} lg={8} xl={8}>
                  <Form.InputNumber
                    field={'CosLogSamplePercent'}
                    label={t('COSLOG 完整内容采样比例 (%)')}
                    extraText={t('支持 0 至 100，最多两位小数')}
                    min={0}
                    max={100}
                    step={0.01}
                    onChange={(value) => {
                      setInputs({
                        ...inputs,
                        CosLogSamplePercent: value,
                      });
                    }}
                  />
                </Col>
              </Row>
              <div
                style={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  gap: 8,
                  marginBottom: 16,
                }}
              >
                {COSLOG_SAMPLE_PRESETS.map((percent) => (
                  <Button
                    key={percent}
                    size='small'
                    theme='borderless'
                    onClick={() => {
                      setInputs({ ...inputs, CosLogSamplePercent: percent });
                      refForm.current.setValue('CosLogSamplePercent', percent);
                    }}
                  >
                    {percent}%
                  </Button>
                ))}
              </div>
              {cosLogStatus && (
                <Descriptions
                  row
                  data={[
                    {
                      key: t('队列深度'),
                      value: `${cosLogStatus.queue_depth} / ${cosLogStatus.queue_capacity}（${t('缓冲')} ${cosLogStatus.buffered_entries} / ${cosLogStatus.flush_size}）`,
                    },
                    {
                      key: t('本地占用'),
                      value: formatBytes(cosLogStatus.local_bytes),
                    },
                    {
                      key: t('最近成功上传'),
                      value: lastUploadText,
                    },
                    {
                      key: t('丢弃数量'),
                      value: (
                        <div
                          style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: 8,
                          }}
                        >
                          <span>{cosLogStatus.dropped_total}</span>
                          <Button
                            size='small'
                            loading={resettingDropped}
                            disabled={cosLogStatus.dropped_total === 0}
                            onClick={resetCosLogDropped}
                          >
                            {t('清空')}
                          </Button>
                        </div>
                      ),
                    },
                  ]}
                />
              )}
            </div>

            <Row>
              <Button size='default' onClick={onSubmit}>
                {t('保存日志设置')}
              </Button>
            </Row>
          </Form.Section>
        </Form>
      </Spin>
    </>
  );
}
