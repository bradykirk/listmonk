<template>
  <section class="analytics">
    <header class="columns page-header">
      <div class="column is-6">
        <h1 class="title is-4">
          List analytics
          <span v-if="cohortTotal" class="has-text-grey-light is-size-6">
            ({{ cohortTotal }} subscribers)
          </span>
        </h1>
      </div>
      <div class="column is-3">
        <b-field label="List">
          <b-select v-model="listID" expanded @input="fetchList">
            <option :value="0">All lists</option>
            <option v-for="l in lists" :key="l.id" :value="l.id">{{ l.name }}</option>
          </b-select>
        </b-field>
      </div>
    </header>
    <hr />

    <!-- Headline numbers. Audience counts follow the list filter; campaign
         rates cover the last 90 days across all lists, because a campaign is
         sent to a set of lists and cannot be attributed to just one. -->
    <div class="columns kpis">
      <div class="column">
        <div class="box kpi">
          <p class="kpi-label">Subscribers</p>
          <p class="kpi-value">{{ (summary.subscribers || 0).toLocaleString() }}</p>
          <p class="kpi-sub">
            <span class="has-text-success">+{{ summary.joined_30d || 0 }}</span>
            /
            <span class="has-text-grey">&minus;{{ summary.unsubscribed_30d || 0 }}</span>
            in 30 days
          </p>
        </div>
      </div>
      <div class="column">
        <div class="box kpi">
          <p class="kpi-label">Emails sent</p>
          <p class="kpi-value">{{ (summary.sent_90d || 0).toLocaleString() }}</p>
          <p class="kpi-sub">{{ summary.campaigns_90d || 0 }} campaigns, 90 days</p>
        </div>
      </div>
      <div class="column">
        <div class="box kpi">
          <p class="kpi-label">Open rate</p>
          <p class="kpi-value">{{ rate(summary.opens_90d, summary.sent_90d).toFixed(1) }}%</p>
          <p class="kpi-sub">inflated by Apple Mail</p>
        </div>
      </div>
      <div class="column">
        <div class="box kpi">
          <p class="kpi-label">Click rate</p>
          <p class="kpi-value">{{ rate(summary.clicks_90d, summary.sent_90d).toFixed(2) }}%</p>
          <p class="kpi-sub">the reliable signal</p>
        </div>
      </div>
      <div class="column">
        <div class="box kpi" :class="healthClass(bounceRate, 2, 5)">
          <p class="kpi-label">Bounce rate</p>
          <p class="kpi-value">{{ bounceRate.toFixed(2) }}%</p>
          <p class="kpi-sub">SES suspends at 5%</p>
        </div>
      </div>
      <div class="column">
        <div class="box kpi" :class="healthClass(complaintRate, 0.05, 0.1)">
          <p class="kpi-label">Complaint rate</p>
          <p class="kpi-value">{{ complaintRate.toFixed(3) }}%</p>
          <p class="kpi-sub">SES suspends at 0.1%</p>
        </div>
      </div>
    </div>

    <div class="columns">
      <div class="column is-8">
        <!-- Audience growth -->
        <div class="box">
          <h4 class="title is-6">Audience growth</h4>
          <p class="has-text-grey is-size-7">New subscribers per week, and the running total.</p>
          <div class="chart-wrap">
            <canvas ref="growthCanvas" />
          </div>
          <p v-if="growth.length === 0" class="has-text-grey">No signups recorded yet.</p>
        </div>

        <!-- Engagement cohorts -->
        <div class="box">
          <h4 class="title is-6">Engagement</h4>
          <p class="has-text-grey is-size-7">
            How recently each subscriber last opened or clicked. Needs
            <strong>individual subscriber tracking</strong> in Settings &rarr; Privacy.
            That setting is not retroactive.
          </p>
          <div class="chart-wrap chart-wrap--short">
            <canvas ref="cohortCanvas" />
          </div>
          <p v-if="cohortTotal === 0" class="has-text-grey">No engagement recorded yet.</p>
        </div>
      </div>

      <!-- Recent activity feed -->
      <div class="column is-4">
        <div class="box activity-box">
          <h4 class="title is-6">
            Recent activity
            <span v-if="selectedCampaign" class="has-text-grey is-size-7">
              &mdash; {{ selectedCampaign.name }}
            </span>
          </h4>
          <p v-if="activity.length === 0" class="has-text-grey is-size-7">
            Nothing recorded. This feed needs individual subscriber tracking.
          </p>
          <ul class="activity-list">
            <li v-for="(a, i) in activity" :key="i">
              <router-link :to="{ name: 'subscribers', query: { id: a.subscriber_id } }">
                {{ a.email }}
              </router-link>
              <span :class="a.action === 'clicked' ? 'has-text-link' : 'has-text-grey'">
                {{ a.action }}
              </span>
              <span class="has-text-grey-light is-size-7">{{ ago(a.created_at) }}</span>
            </li>
          </ul>
        </div>
      </div>
    </div>

    <!-- Campaign performance -->
    <div class="box">
      <h4 class="title is-6">Campaign performance</h4>
      <p class="has-text-grey is-size-7">
        Select a campaign to see its first 48 hours and its most clicked links.
        Open rates count Apple Mail pre-fetches, so clicks are the more reliable signal.
      </p>
      <b-table :data="campaigns" :hoverable="true" :selected.sync="selectedCampaign" narrowed
        @click="selectCampaign">
        <b-table-column v-slot="props" field="name" label="Campaign">
          {{ props.row.name }}
          <p class="has-text-grey is-size-7">{{ props.row.subject }}</p>
        </b-table-column>
        <b-table-column v-slot="props" field="started_at" label="Sent">
          {{ props.row.started_at ? props.row.started_at.substring(0, 10) : '—' }}
        </b-table-column>
        <b-table-column v-slot="props" field="sent" label="Sent to" numeric>
          {{ props.row.sent }}
        </b-table-column>
        <b-table-column v-slot="props" field="unique_opens" label="Opened" numeric>
          {{ props.row.unique_opens }}
          <span class="has-text-grey is-size-7">{{ pct(props.row.unique_opens, props.row.sent) }}</span>
        </b-table-column>
        <b-table-column v-slot="props" label="Didn't open" numeric>
          <span class="has-text-grey">{{ notCount(props.row.sent, props.row.unique_opens) }}</span>
        </b-table-column>
        <b-table-column v-slot="props" field="unique_clicks" label="Clicked" numeric>
          {{ props.row.unique_clicks }}
          <span class="has-text-grey is-size-7">{{ pct(props.row.unique_clicks, props.row.sent) }}</span>
        </b-table-column>
        <b-table-column v-slot="props" label="Didn't click" numeric>
          <span class="has-text-grey">{{ notCount(props.row.sent, props.row.unique_clicks) }}</span>
        </b-table-column>
        <b-table-column v-slot="props" field="unsubscribes" label="Unsub.*" numeric>
          <span class="has-text-grey">
            {{ props.row.unsubscribes }}
            <span class="is-size-7">{{ pct(props.row.unsubscribes, props.row.sent) }}</span>
          </span>
        </b-table-column>
        <b-table-column v-slot="props" field="bounces" label="Bounced" numeric>
          <span :class="{ 'has-text-danger': rate(props.row.bounces, props.row.sent) > 5 }">
            {{ props.row.bounces }}
            <span class="is-size-7">{{ pct(props.row.bounces, props.row.sent) }}</span>
          </span>
          <p v-if="props.row.bounces" class="has-text-grey is-size-7">
            {{ props.row.hard_bounces }} hard / {{ props.row.soft_bounces }} soft
          </p>
        </b-table-column>
        <b-table-column v-slot="props" field="complaints" label="Complained" numeric>
          <span :class="{ 'has-text-danger': rate(props.row.complaints, props.row.sent) > 0.1 }">
            {{ props.row.complaints }}
            <span class="is-size-7">{{ pct(props.row.complaints, props.row.sent) }}</span>
          </span>
        </b-table-column>
        <template #empty>
          <p class="has-text-grey">No finished campaigns yet.</p>
        </template>
      </b-table>
      <p class="has-text-grey is-size-7 mt-3">
        * Unsubscribes are <strong>attributed, not exact</strong>. listmonk records an
        unsubscribe on the subscription without a reference to the campaign that caused
        it, so this counts unsubscribes from the campaign's own lists in the 72 hours
        after it started. Two campaigns to the same list inside one window will both
        claim the same unsubscribe.
      </p>
    </div>

    <!-- Deliverability trend -->
    <div class="box">
      <h4 class="title is-6">Deliverability trend</h4>
      <p class="has-text-grey is-size-7">
        The same campaigns over time. A bounce or complaint line that is climbing is
        the earliest warning that a list needs cleaning &mdash; it shows up here well
        before Amazon acts on it.
      </p>
      <div class="chart-wrap">
        <canvas ref="trendCanvas" />
      </div>
    </div>

    <div class="columns">
      <!-- Mailbox providers -->
      <div class="column is-7">
        <div class="box">
          <h4 class="title is-6">Mailbox providers</h4>
          <p class="has-text-grey is-size-7">
            Share of each provider's subscribers who have ever opened or clicked. One
            provider engaging far below the rest usually means it is filtering to spam,
            which an overall open rate hides.
          </p>
          <b-table :data="domains" narrowed>
            <b-table-column v-slot="props" field="domain" label="Provider">
              {{ props.row.domain }}
              <b-tag v-if="isUnderperforming(props.row)" type="is-warning" class="ml-2">
                low engagement
              </b-tag>
            </b-table-column>
            <b-table-column v-slot="props" field="subscribers" label="Subscribers" numeric>
              {{ props.row.subscribers.toLocaleString() }}
            </b-table-column>
            <b-table-column v-slot="props" field="openers" label="Ever opened" numeric>
              {{ rate(props.row.openers, props.row.subscribers).toFixed(1) }}%
            </b-table-column>
            <b-table-column v-slot="props" field="clickers" label="Ever clicked" numeric>
              {{ rate(props.row.clickers, props.row.subscribers).toFixed(1) }}%
            </b-table-column>
            <b-table-column v-slot="props" field="bounced" label="Bounced" numeric>
              <span :class="{ 'has-text-danger': rate(props.row.bounced, props.row.subscribers) > 5 }">
                {{ rate(props.row.bounced, props.row.subscribers).toFixed(1) }}%
              </span>
            </b-table-column>
            <template #empty>
              <p class="has-text-grey">No subscribers yet.</p>
            </template>
          </b-table>
        </div>
      </div>

      <!-- Send times -->
      <div class="column is-5">
        <div class="box">
          <h4 class="title is-6">Best send times</h4>
          <p class="has-text-grey is-size-7">
            Open rate by the weekday and hour a campaign went out, in the server's
            timezone. Slots covering a single campaign are marked: they show that
            campaign, not a pattern.
          </p>
          <b-table :data="sendTimes" narrowed>
            <b-table-column v-slot="props" label="Slot">
              {{ dayName(props.row.dow) }} {{ hourLabel(props.row.hour) }}
              <b-tag v-if="props.row.campaigns < 3" type="is-light" class="ml-2">
                {{ props.row.campaigns }} campaign{{ props.row.campaigns === 1 ? '' : 's' }}
              </b-tag>
            </b-table-column>
            <b-table-column v-slot="props" field="sent" label="Sent" numeric>
              {{ props.row.sent.toLocaleString() }}
            </b-table-column>
            <b-table-column v-slot="props" field="opens" label="Open rate" numeric>
              {{ rate(props.row.opens, props.row.sent).toFixed(1) }}%
            </b-table-column>
            <template #empty>
              <p class="has-text-grey">No finished campaigns yet.</p>
            </template>
          </b-table>
        </div>
      </div>
    </div>

    <!-- Selected campaign detail -->
    <div v-if="selectedCampaign" class="box">
      <h4 class="title is-6">{{ selectedCampaign.name }} &mdash; first 48 hours</h4>
      <div class="chart-wrap">
        <canvas ref="timelineCanvas" />
      </div>

      <h4 class="title is-6 mt-5">Top links clicked</h4>
      <b-table :data="links" narrowed>
        <b-table-column v-slot="props" field="url" label="URL">
          <a :href="props.row.url" target="_blank" rel="noopener">{{ props.row.url }}</a>
        </b-table-column>
        <b-table-column v-slot="props" field="clicks" label="Clicks" numeric>
          {{ props.row.clicks }}
        </b-table-column>
        <b-table-column v-slot="props" field="unique_clickers" label="Unique" numeric>
          {{ props.row.unique_clickers }}
        </b-table-column>
        <template #empty>
          <p class="has-text-grey">No clicks recorded for this campaign.</p>
        </template>
      </b-table>
    </div>
  </section>
</template>

<script>
import Chart from 'chart.js/auto';
import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';
import * as api from '../api';

dayjs.extend(relativeTime);

// Cohort keys carry a numeric prefix so SQL can sort them. Map them to labels.
const COHORT_LABELS = {
  '0_never': 'Never engaged',
  '1_active_30d': 'Active < 30 days',
  '2_active_90d': 'Active 30-90 days',
  '3_active_180d': 'Active 90-180 days',
  '4_cold': 'Cold 180 days+',
};

const COHORT_COLOURS = ['#b0b0b0', '#0055d4', '#4d8fdc', '#f0a92e', '#d13b3b'];

export default {
  name: 'Analytics',

  data() {
    return {
      listID: 0,
      lists: [],
      growth: [],
      cohorts: [],
      campaigns: [],
      activity: [],
      links: [],
      timeline: [],
      summary: {},
      domains: [],
      sendTimes: [],
      selectedCampaign: null,
      charts: {
        growth: null, cohort: null, timeline: null, trend: null,
      },
    };
  },

  computed: {
    cohortTotal() {
      return this.cohorts.reduce((acc, c) => acc + c.subscribers, 0);
    },

    bounceRate() {
      return this.rate(this.summary.bounces_90d, this.summary.sent_90d);
    },

    complaintRate() {
      return this.rate(this.summary.complaints_90d, this.summary.sent_90d);
    },

    // The engagement rate across every provider, used as the baseline that an
    // individual provider is judged against.
    overallOpenRate() {
      const subs = this.domains.reduce((acc, d) => acc + d.subscribers, 0);
      const openers = this.domains.reduce((acc, d) => acc + d.openers, 0);
      return this.rate(openers, subs);
    },
  },

  methods: {
    rate(n, total) {
      return total ? (n / total) * 100 : 0;
    },

    pct(n, total) {
      return total ? `(${((n / total) * 100).toFixed(1)}%)` : '';
    },

    // Recipients who did not perform an action. listmonk keeps no per-campaign
    // recipient list, so this is derived from the send count.
    notCount(sent, did) {
      return Math.max(0, (sent || 0) - (did || 0));
    },

    ago(ts) {
      return dayjs(ts).fromNow();
    },

    // Colours a KPI box against the thresholds that get a sender suspended.
    healthClass(value, warnAt, dangerAt) {
      if (value >= dangerAt) {
        return 'kpi--danger';
      }
      if (value >= warnAt) {
        return 'kpi--warn';
      }
      return '';
    },

    // A provider engaging at less than half the overall rate is worth looking at.
    // Small providers are skipped: a 12-subscriber domain proves nothing.
    isUnderperforming(row) {
      if (row.subscribers < 25 || this.overallOpenRate === 0) {
        return false;
      }
      return this.rate(row.openers, row.subscribers) < this.overallOpenRate / 2;
    },

    dayName(dow) {
      return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][dow] || '—';
    },

    hourLabel(h) {
      if (h === 0) {
        return '12am';
      }
      if (h === 12) {
        return '12pm';
      }
      return h < 12 ? `${h}am` : `${h - 12}pm`;
    },

    destroyChart(key) {
      if (this.charts[key]) {
        this.charts[key].destroy();
        this.charts[key] = null;
      }
    },

    // List-scoped data: growth, cohorts, headline counts and provider split.
    async fetchList() {
      const params = { list_id: this.listID };
      const [growth, cohorts, summary, domains] = await Promise.all([
        api.getAnalyticsGrowth(params),
        api.getAnalyticsCohorts(params),
        api.getAnalyticsSummary(params),
        api.getAnalyticsDomains(params),
      ]);
      this.growth = growth || [];
      this.cohorts = cohorts || [];
      this.summary = summary || {};
      this.domains = domains || [];

      this.$nextTick(() => {
        this.renderGrowth();
        this.renderCohorts();
      });
    },

    async selectCampaign(row) {
      if (!row) {
        return;
      }
      this.selectedCampaign = row;

      const params = { campaign_id: row.id };
      const [timeline, links, activity] = await Promise.all([
        api.getAnalyticsCampaignTimeline(params),
        api.getAnalyticsCampaignLinks(params),
        api.getAnalyticsActivity(params),
      ]);
      this.timeline = timeline || [];
      this.links = links || [];
      this.activity = activity || [];

      this.$nextTick(() => this.renderTimeline());
    },

    renderGrowth() {
      this.destroyChart('growth');
      if (!this.$refs.growthCanvas || this.growth.length === 0) {
        return;
      }

      this.charts.growth = new Chart(this.$refs.growthCanvas, {
        data: {
          labels: this.growth.map((r) => r.week),
          datasets: [
            {
              type: 'bar',
              label: 'New per week',
              data: this.growth.map((r) => r.joined),
              backgroundColor: '#4d8fdc',
              yAxisID: 'y',
            },
            {
              type: 'line',
              label: 'Total list size',
              data: this.growth.map((r) => r.cumulative),
              borderColor: '#0055d4',
              backgroundColor: 'transparent',
              tension: 0.3,
              yAxisID: 'y1',
            },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          interaction: { mode: 'index', intersect: false },
          scales: {
            y: { beginAtZero: true, title: { display: true, text: 'New' } },
            y1: {
              beginAtZero: true,
              position: 'right',
              grid: { drawOnChartArea: false },
              title: { display: true, text: 'Total' },
            },
          },
        },
      });
    },

    renderCohorts() {
      this.destroyChart('cohort');
      if (!this.$refs.cohortCanvas || this.cohorts.length === 0) {
        return;
      }

      this.charts.cohort = new Chart(this.$refs.cohortCanvas, {
        type: 'bar',
        data: {
          labels: this.cohorts.map((c) => COHORT_LABELS[c.cohort] || c.cohort),
          datasets: [{
            label: 'Subscribers',
            data: this.cohorts.map((c) => c.subscribers),
            backgroundColor: this.cohorts.map((c, i) => COHORT_COLOURS[i % COHORT_COLOURS.length]),
          }],
        },
        options: {
          indexAxis: 'y',
          responsive: true,
          maintainAspectRatio: false,
          plugins: { legend: { display: false } },
          scales: { x: { beginAtZero: true } },
        },
      });
    },

    renderTimeline() {
      this.destroyChart('timeline');
      if (!this.$refs.timelineCanvas || this.timeline.length === 0) {
        return;
      }

      this.charts.timeline = new Chart(this.$refs.timelineCanvas, {
        type: 'line',
        data: {
          labels: this.timeline.map((r) => `+${r.hour}h`),
          datasets: [
            {
              label: 'Opened',
              data: this.timeline.map((r) => r.opens),
              borderColor: '#0055d4',
              backgroundColor: 'transparent',
              tension: 0.3,
            },
            {
              label: 'Clicked',
              data: this.timeline.map((r) => r.clicks),
              borderColor: '#4bb37b',
              backgroundColor: 'transparent',
              tension: 0.3,
            },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          interaction: { mode: 'index', intersect: false },
          scales: { y: { beginAtZero: true } },
        },
      });
    },

    // Rates per campaign, oldest first. Bounce and complaint sit on their own
    // axis: at healthy levels they are a fraction of a percent and would be a
    // flat line against an open rate in the twenties.
    renderTrend() {
      this.destroyChart('trend');
      if (!this.$refs.trendCanvas || this.campaigns.length === 0) {
        return;
      }

      const rows = [...this.campaigns].reverse();
      const series = (key) => rows.map((r) => this.rate(r[key], r.sent));

      this.charts.trend = new Chart(this.$refs.trendCanvas, {
        type: 'line',
        data: {
          labels: rows.map((r) => (r.started_at ? r.started_at.substring(0, 10) : r.name)),
          datasets: [
            {
              label: 'Open %',
              data: series('unique_opens'),
              borderColor: '#0055d4',
              backgroundColor: 'transparent',
              tension: 0.3,
            },
            {
              label: 'Click %',
              data: series('unique_clicks'),
              borderColor: '#4bb37b',
              backgroundColor: 'transparent',
              tension: 0.3,
            },
            {
              label: 'Bounce %',
              data: series('bounces'),
              borderColor: '#f0a92e',
              backgroundColor: 'transparent',
              tension: 0.3,
              yAxisID: 'y1',
            },
            {
              label: 'Complaint %',
              data: series('complaints'),
              borderColor: '#d13b3b',
              backgroundColor: 'transparent',
              tension: 0.3,
              yAxisID: 'y1',
            },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          interaction: { mode: 'index', intersect: false },
          scales: {
            y: {
              beginAtZero: true,
              title: { display: true, text: 'Open / click %' },
            },
            y1: {
              beginAtZero: true,
              position: 'right',
              grid: { drawOnChartArea: false },
              title: { display: true, text: 'Bounce / complaint %' },
            },
          },
        },
      });
    },
  },

  async mounted() {
    const lists = await api.getLists({ minimal: true, per_page: 'all' });
    this.lists = (lists && lists.results) || lists || [];

    const [campaigns, activity, sendTimes] = await Promise.all([
      api.getAnalyticsCampaigns(),
      api.getAnalyticsActivity({ campaign_id: 0 }),
      api.getAnalyticsSendTimes(),
    ]);
    this.campaigns = campaigns || [];
    this.activity = activity || [];

    // Best slot first. The table marks thin slots rather than hiding them.
    this.sendTimes = (sendTimes || []).slice().sort(
      (a, b) => this.rate(b.opens, b.sent) - this.rate(a.opens, a.sent),
    );

    await this.fetchList();
    this.$nextTick(() => this.renderTrend());
  },

  beforeDestroy() {
    Object.keys(this.charts).forEach((k) => this.destroyChart(k));
  },
};
</script>

<style scoped>
.kpis {
  flex-wrap: wrap;
}

.kpi {
  height: 100%;
  padding: 0.9rem 1rem;
  border-top: 3px solid transparent;
}

.kpi--warn {
  border-top-color: #f0a92e;
}

.kpi--danger {
  border-top-color: #d13b3b;
}

.kpi-label {
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: #7a7a7a;
}

.kpi-value {
  font-size: 1.6rem;
  font-weight: 700;
  line-height: 1.25;
}

.kpi-sub {
  font-size: 0.72rem;
  color: #9a9a9a;
}

.chart-wrap {
  position: relative;
  height: 320px;
  margin-top: 1rem;
}

.chart-wrap--short {
  height: 220px;
}

.activity-box {
  max-height: 720px;
  overflow-y: auto;
}

.activity-list {
  margin-top: 0.75rem;
}

.activity-list li {
  padding: 0.4rem 0;
  border-bottom: 1px solid #f0f0f0;
  font-size: 0.86rem;
  line-height: 1.35;
}

.activity-list li span {
  display: inline-block;
  margin-left: 0.35rem;
}
</style>
