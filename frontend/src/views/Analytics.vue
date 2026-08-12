<template>
  <section class="analytics">
    <header class="columns page-header">
      <div class="column is-6">
        <h1 class="title is-4">
          List analytics
          <span v-if="totalSubscribers !== null" class="has-text-grey-light is-size-6">
            ({{ totalSubscribers }} on list)
          </span>
        </h1>
      </div>
      <div class="column is-3">
        <b-field label="List">
          <b-select v-model="listID" @input="fetchAll" expanded>
            <option :value="0">All lists</option>
            <option v-for="l in lists" :key="l.id" :value="l.id">{{ l.name }}</option>
          </b-select>
        </b-field>
      </div>
    </header>
    <hr />

    <!-- Audience growth -->
    <div class="box">
      <h4 class="title is-6">Audience growth</h4>
      <p class="has-text-grey is-size-7">
        New subscribers per week, and the running total.
      </p>
      <div class="chart-wrap">
        <canvas ref="growthCanvas" />
      </div>
      <p v-if="growth.length === 0" class="has-text-grey">No signups recorded yet.</p>
    </div>

    <!-- Engagement cohorts -->
    <div class="box">
      <h4 class="title is-6">Engagement</h4>
      <p class="has-text-grey is-size-7">
        How recently each subscriber last opened or clicked. Requires
        <strong>individual subscriber tracking</strong> in Settings &rarr; Privacy.
        It is not retroactive.
      </p>
      <div class="chart-wrap chart-wrap--short">
        <canvas ref="cohortCanvas" />
      </div>
      <p v-if="cohortTotal === 0" class="has-text-grey">
        No engagement recorded yet.
      </p>
    </div>

    <!-- Campaign performance -->
    <div class="box">
      <h4 class="title is-6">Campaign performance</h4>
      <p class="has-text-grey is-size-7">
        Unique opens and clicks per finished campaign. Open rates count Apple Mail
        pre-fetches, so treat clicks as the more reliable signal.
      </p>
      <b-table :data="campaigns" :hoverable="true" default-sort="started_at" narrowed>
        <b-table-column field="name" label="Campaign" v-slot="props">
          <router-link :to="{ name: 'campaign', params: { id: props.row.id } }">
            {{ props.row.name }}
          </router-link>
          <p class="has-text-grey is-size-7">{{ props.row.subject }}</p>
        </b-table-column>
        <b-table-column field="started_at" label="Sent" v-slot="props">
          {{ props.row.started_at ? props.row.started_at.substring(0, 10) : '—' }}
        </b-table-column>
        <b-table-column field="sent" label="Sent to" numeric v-slot="props">
          {{ props.row.sent }}
        </b-table-column>
        <b-table-column field="unique_opens" label="Opens" numeric v-slot="props">
          {{ props.row.unique_opens }}
          <span class="has-text-grey is-size-7">({{ pct(props.row.unique_opens, props.row.sent) }})</span>
        </b-table-column>
        <b-table-column field="unique_clicks" label="Clicks" numeric v-slot="props">
          {{ props.row.unique_clicks }}
          <span class="has-text-grey is-size-7">({{ pct(props.row.unique_clicks, props.row.sent) }})</span>
        </b-table-column>
        <b-table-column field="bounces" label="Bounces" numeric v-slot="props">
          <span :class="{ 'has-text-danger': bounceRate(props.row) > 5 }">
            {{ props.row.bounces }}
            <span class="is-size-7">({{ pct(props.row.bounces, props.row.sent) }})</span>
          </span>
        </b-table-column>
        <template #empty>
          <p class="has-text-grey">No finished campaigns yet.</p>
        </template>
      </b-table>
    </div>
  </section>
</template>

<script>
import Chart from 'chart.js/auto';
import * as api from '../api';

// Cohort keys are prefixed so that SQL can sort them. Map them to labels here.
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
      growthChart: null,
      cohortChart: null,
    };
  },

  computed: {
    cohortTotal() {
      return this.cohorts.reduce((acc, c) => acc + c.subscribers, 0);
    },

    totalSubscribers() {
      return this.cohortTotal || null;
    },
  },

  methods: {
    // Percentage of a total, guarding against a zero send count.
    pct(n, total) {
      if (!total) {
        return '—';
      }
      return `${((n / total) * 100).toFixed(1)}%`;
    },

    bounceRate(row) {
      return row.sent ? (row.bounces / row.sent) * 100 : 0;
    },

    async fetchAll() {
      const params = { list_id: this.listID };
      const [growth, cohorts, campaigns] = await Promise.all([
        api.getAnalyticsGrowth(params),
        api.getAnalyticsCohorts(params),
        api.getAnalyticsCampaigns(),
      ]);

      this.growth = growth || [];
      this.cohorts = cohorts || [];
      this.campaigns = campaigns || [];

      this.$nextTick(() => {
        this.renderGrowth();
        this.renderCohorts();
      });
    },

    renderGrowth() {
      if (this.growthChart) {
        this.growthChart.destroy();
      }
      if (!this.$refs.growthCanvas || this.growth.length === 0) {
        return;
      }

      this.growthChart = new Chart(this.$refs.growthCanvas, {
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
      if (this.cohortChart) {
        this.cohortChart.destroy();
      }
      if (!this.$refs.cohortCanvas || this.cohorts.length === 0) {
        return;
      }

      this.cohortChart = new Chart(this.$refs.cohortCanvas, {
        type: 'bar',
        data: {
          labels: this.cohorts.map((c) => COHORT_LABELS[c.cohort] || c.cohort),
          datasets: [{
            label: 'Subscribers',
            data: this.cohorts.map((c) => c.subscribers),
            backgroundColor: this.cohorts.map(
              (c, i) => COHORT_COLOURS[i % COHORT_COLOURS.length],
            ),
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
  },

  async mounted() {
    const lists = await api.getLists({ minimal: true, per_page: 'all' });
    this.lists = (lists && lists.results) || lists || [];
    await this.fetchAll();
  },

  beforeDestroy() {
    if (this.growthChart) {
      this.growthChart.destroy();
    }
    if (this.cohortChart) {
      this.cohortChart.destroy();
    }
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
</style>
