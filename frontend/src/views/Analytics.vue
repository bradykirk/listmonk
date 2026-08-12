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
        <b-table-column v-slot="props" field="bounces" label="Bounced" numeric>
          <span :class="{ 'has-text-danger': rate(props.row.bounces, props.row.sent) > 5 }">
            {{ props.row.bounces }}
            <span class="is-size-7">{{ pct(props.row.bounces, props.row.sent) }}</span>
          </span>
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
        Unsubscribes are not shown per campaign. listmonk records an unsubscribe on the
        subscription, without a reference to the campaign that caused it, so the number
        cannot be recovered.
      </p>
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
      selectedCampaign: null,
      charts: { growth: null, cohort: null, timeline: null },
    };
  },

  computed: {
    cohortTotal() {
      return this.cohorts.reduce((acc, c) => acc + c.subscribers, 0);
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

    destroyChart(key) {
      if (this.charts[key]) {
        this.charts[key].destroy();
        this.charts[key] = null;
      }
    },

    // List-scoped data: growth, cohorts and the global activity feed.
    async fetchList() {
      const params = { list_id: this.listID };
      const [growth, cohorts] = await Promise.all([
        api.getAnalyticsGrowth(params),
        api.getAnalyticsCohorts(params),
      ]);
      this.growth = growth || [];
      this.cohorts = cohorts || [];

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
  },

  async mounted() {
    const lists = await api.getLists({ minimal: true, per_page: 'all' });
    this.lists = (lists && lists.results) || lists || [];

    const [campaigns, activity] = await Promise.all([
      api.getAnalyticsCampaigns(),
      api.getAnalyticsActivity({ campaign_id: 0 }),
    ]);
    this.campaigns = campaigns || [];
    this.activity = activity || [];

    await this.fetchList();
  },

  beforeDestroy() {
    Object.keys(this.charts).forEach((k) => this.destroyChart(k));
  },
};
</script>

<style scoped>
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
