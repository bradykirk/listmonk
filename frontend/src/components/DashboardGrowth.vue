<template>
  <!-- Audience growth (gunmade fork). Signups and audience size over a range,
       in the viewer's time zone. Data: GET /api/dashboard/growth. -->
  <article class="dashboard-growth notification relative" data-cy="growth">
    <b-loading :active="isLoading" :is-full-page="false" />

    <div class="growth-head">
      <h3 class="title is-size-6">Audience growth</h3>
      <b-field class="growth-range">
        <b-radio-button v-for="r in ranges" :key="r.value" v-model="range" :native-value="r.value"
          :name="`growth-range-${_uid}`" size="is-small" type="is-primary">
          {{ r.label }}
        </b-radio-button>
      </b-field>
    </div>

    <div class="columns is-mobile is-multiline growth-kpis">
      <div class="column is-6-mobile is-3-tablet">
        <p class="growth-kpi-label">Audience now</p>
        <p class="growth-kpi-value">{{ $utils.niceNumber(data.audienceNow || 0) }}</p>
      </div>
      <div class="column is-6-mobile is-3-tablet">
        <p class="growth-kpi-label">New signups</p>
        <p class="growth-kpi-value">+{{ $utils.niceNumber(data.signups || 0) }}</p>
      </div>
      <div class="column is-6-mobile is-3-tablet">
        <p class="growth-kpi-label">Unsubscribes</p>
        <p class="growth-kpi-value">&minus;{{ $utils.niceNumber(data.unsubscribes || 0) }}</p>
      </div>
      <div class="column is-6-mobile is-3-tablet">
        <p class="growth-kpi-label">Net growth</p>
        <p class="growth-kpi-value" :class="netClass">{{ netText }}</p>
      </div>
    </div>

    <p v-if="isError" class="has-text-danger">Could not load audience growth.</p>
    <p v-else-if="!isLoading && !hasPeople" class="has-text-grey">No signups yet.</p>
    <div v-show="!isError && hasPeople" class="growth-chart">
      <canvas ref="canvas" />
    </div>

    <p class="growth-note is-size-7 has-text-grey">
      Each person counts once, on the day they were added, imports included. Days follow your
      time zone ({{ tz || 'UTC' }}). Unsubscribe dates and past audience sizes are rebuilt from
      current data, so they are approximate: deleted subscribers and removed list memberships are
      not in the history.
    </p>
  </article>
</template>

<script>
import Chart from 'chart.js/auto';
import dayjs from 'dayjs';
import Vue from 'vue';
import { colors } from '../constants';

const BAR_COLOR = '#4d8fdc';

export default Vue.extend({
  name: 'DashboardGrowth',

  data() {
    return {
      ranges: [
        { value: '7d', label: '7 days' },
        { value: '30d', label: '30 days' },
        { value: '90d', label: '90 days' },
        { value: '12m', label: '12 months' },
      ],
      range: '30d',
      tz: this.browserTZ(),
      data: {},
      isLoading: false,
      isError: false,

      // Incremented per request so that a slow response for an old range
      // cannot overwrite the data for the range selected after it.
      reqID: 0,
    };
  },

  // The Chart instance is kept off the reactive data on purpose: Vue 2 would
  // otherwise walk and observe chart.js internals.
  chart: null,

  computed: {
    series() {
      return this.data.series || [];
    },

    hasPeople() {
      return this.series.some((p) => p.audience > 0 || p.signups > 0 || p.unsubscribes > 0);
    },

    netText() {
      const n = this.data.net || 0;
      if (n > 0) {
        return `+${this.$utils.niceNumber(n)}`;
      }
      if (n < 0) {
        return `−${this.$utils.niceNumber(-n)}`;
      }
      return '0';
    },

    netClass() {
      const n = this.data.net || 0;
      return { 'has-text-success': n > 0, 'has-text-danger': n < 0 };
    },
  },

  watch: {
    range() {
      this.fetchData();
    },
  },

  methods: {
    browserTZ() {
      try {
        return Intl.DateTimeFormat().resolvedOptions().timeZone || '';
      } catch (e) {
        return '';
      }
    },

    fetchData() {
      this.reqID += 1;
      const id = this.reqID;

      const params = { range: this.range };
      if (this.tz) {
        params.tz = this.tz;
      }

      this.isLoading = true;
      this.isError = false;

      this.$api.getDashboardGrowth(params).then((data) => {
        if (id !== this.reqID) {
          return;
        }
        this.data = data || {};
        this.isLoading = false;
        this.$nextTick(this.renderChart);
      }).catch(() => {
        if (id !== this.reqID) {
          return;
        }
        this.data = {};
        this.isError = true;
        this.isLoading = false;
        this.destroyChart();
      });
    },

    destroyChart() {
      if (this.$options.chart) {
        this.$options.chart.destroy();
        this.$options.chart = null;
      }
    },

    renderChart() {
      this.destroyChart();
      if (!this.$refs.canvas || !this.hasPeople) {
        return;
      }

      const { series } = this;
      const weekly = this.data.bucket === 'week';

      this.$options.chart = new Chart(this.$refs.canvas, {
        data: {
          labels: series.map((p) => dayjs(p.date).format('DD MMM')),
          datasets: [
            {
              type: 'line',
              label: 'Audience size',
              data: series.map((p) => p.audience),
              borderColor: colors.primary,
              backgroundColor: 'transparent',
              borderWidth: 2,
              pointRadius: 0,
              pointHoverRadius: 4,
              tension: 0.3,
              yAxisID: 'y1',
              order: 1,
            },
            {
              type: 'bar',
              label: weekly ? 'Signups per week' : 'Signups per day',
              data: series.map((p) => p.signups),
              backgroundColor: BAR_COLOR,
              borderRadius: 2,
              yAxisID: 'y',
              order: 2,
            },
          ],
        },
        options: {
          responsive: true,
          maintainAspectRatio: false,
          animation: false,
          interaction: { mode: 'index', intersect: false },
          plugins: {
            legend: { position: 'bottom', labels: { boxWidth: 12 } },
            tooltip: {
              callbacks: {
                title: (items) => {
                  const p = series[items[0].dataIndex];
                  return weekly
                    ? `Week of ${dayjs(p.date).format('DD MMM YYYY')}`
                    : dayjs(p.date).format('ddd, DD MMM YYYY');
                },
                afterBody: (items) => `Unsubscribes: ${series[items[0].dataIndex].unsubscribes}`,
              },
            },
          },
          scales: {
            x: { grid: { display: false }, ticks: { maxTicksLimit: 10, maxRotation: 0 } },
            y: {
              position: 'left',
              beginAtZero: true,
              ticks: { precision: 0 },
              title: { display: true, text: 'Signups' },
            },
            y1: {
              position: 'right',
              grid: { drawOnChartArea: false },
              ticks: { precision: 0 },
              title: { display: true, text: 'Audience' },
            },
          },
        },
      });
    },
  },

  created() {
    this.$root.$on('page.refresh', this.fetchData);
  },

  mounted() {
    this.fetchData();
  },

  beforeDestroy() {
    this.$root.$off('page.refresh', this.fetchData);
    this.destroyChart();
  },
});
</script>

<style scoped>
.dashboard-growth {
  margin-bottom: 1.5rem;
}

.growth-head {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}

.growth-head .title {
  margin-bottom: 0;
}

.growth-range {
  margin-bottom: 0;
}

.growth-kpis {
  margin-bottom: 0.5rem;
}

.growth-kpi-label {
  font-size: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: #7f8c8d;
  margin-bottom: 0.1rem;
}

.growth-kpi-value {
  font-size: 1.6rem;
  font-weight: 600;
  line-height: 1.2;
}

.growth-chart {
  position: relative;
  height: 260px;
}

.growth-note {
  margin-top: 0.75rem;
}
</style>
